package ptytest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

// Output arrives independently of terminal input, which scrollback consumes.
func controlledScrollOutput(t *testing.T) (string, func(int)) {
	t.Helper()
	dir := t.TempDir()
	fifo := filepath.Join(dir, "output")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	pipe, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipe.Close() })
	script := filepath.Join(dir, "shell")
	source := "#!/bin/sh\nprintf '\\nREADY\\n'\ni=0\nwhile IFS= read -r count; do\n end=$((i + count))\n while [ $i -lt $end ]; do\n i=$((i + 1))\n printf 'ROW-%05d\\n' \"$i\"\n done\ndone < '" + strings.ReplaceAll(fifo, "'", "'\\''") + "'\n"
	if err := os.WriteFile(script, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return script, func(n int) {
		t.Helper()
		if _, err := fmt.Fprintln(pipe, n); err != nil {
			t.Fatal(err)
		}
	}
}

func scrollFrameContains(f *protocolpb.Frame, text string) bool {
	contains := func(values []uint32) bool {
		runes := make([]rune, len(values))
		for i, value := range values {
			runes[i] = rune(value)
		}
		return strings.Contains(string(runes), text)
	}
	if snap := f.GetSnapshot(); snap != nil && contains(snap.Runes) {
		return true
	}
	if diff := f.GetDiff(); diff != nil {
		for _, row := range diff.DiffRows {
			if contains(row.Runes) {
				return true
			}
		}
	}
	return false
}

func visibleScrollRow(t *testing.T, screen ptytest.Screen) int {
	t.Helper()
	row := strings.TrimSpace(screen.Row(1))
	if !strings.HasPrefix(row, "ROW-") {
		t.Fatalf("expected numbered row, got %q", row)
	}
	value, err := strconv.Atoi(strings.TrimPrefix(row, "ROW-"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestLiveScrollbackKeepsViewedRowsHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach", "multi"} {
		for _, initial := range []int{200, 1500} {
			t.Run(fmt.Sprintf("%s/rows=%d", mode, initial), func(t *testing.T) {
				clk := clock.NewMock()
				recorder := ptytest.NewWSRecorder()
				h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
				shell, emit := controlledScrollOutput(t)
				var awaited atomic.Int64
				var historyRows atomic.Int64
				historyReady := make(chan struct{}, 1)
				delivered := make(chan int64, 32)
				observe := func(f *protocolpb.Frame) {
					if history := f.GetScrollback(); history != nil {
						if history.Clear {
							historyRows.Store(0)
						}
						if historyRows.Add(int64(len(history.Rows))) >= int64(min(initial-10, 1337)) {
							select {
							case historyReady <- struct{}{}:
							default:
							}
						}
					}
					target := awaited.Load()
					if target > 0 && scrollFrameContains(f, fmt.Sprintf("ROW-%05d", target)) {
						select {
						case delivered <- target:
						default:
						}
					}
				}
				hostObserve := observe
				if mode != "host" {
					hostObserve = nil
				}
				host := h.StartHost(ptytest.HostOptions{SessionID: "anchored", SessionName: "anchored", Cols: 60, Rows: 12, Shell: shell, OnPublishFrame: hostObserve})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY") })
				waitRestoreHost(t, recorder, "anchored")
				terminal := host
				if mode == "attach" {
					terminal = h.StartAttach(ptytest.AttachOptions{SessionID: "anchored", Cols: 60, Rows: 12, OnFrame: observe})
				} else if mode == "multi" {
					terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "anchored", Cols: 60, Rows: 12, OnView: func(_ string, c *attach.Client) { c.OnFrame = observe }})
				}
				if terminal != host {
					t.Cleanup(terminal.Cancel)
				}
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("READY") })
				count := 0
				output := func(n int) {
					t.Helper()
					count += n
					awaited.Store(int64(count))
					emit(n)
					ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
					defer cancel()
					for {
						select {
						case value := <-delivered:
							if value == int64(count) {
								return
							}
						case <-ctx.Done():
							t.Fatal("output processing barrier:", ctx.Err())
						}
					}
				}
				for remaining := initial; remaining > 0; {
					chunk := min(20, remaining)
					output(chunk)
					remaining -= chunk
				}
				select {
				case <-historyReady:
				case <-time.After(8 * time.Second):
					t.Fatal("initial history delivery barrier")
				}
				// A second numbered burst fences processing of the final history
				// frame, including initial replay, before navigation starts.
				output(1)
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("ROW-%05d", count)) })
				terminal.Send("\x0c[")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
					return strings.Contains(s.Row(0), "[%") || strings.Contains(s.Row(0), "%]")
				})
				terminal.Send("g" + strings.Repeat("j", 30))
				// Fence all queued navigation with its final expected top row. At capacity
				// only the newest 1337 history rows remain, followed by twelve live rows.
				want := 30 // Empty first row and READY precede numbered output.
				if count-11 > 1337 {
					oldest := count - 11 - 1337 + 1
					want = oldest + 31
				}
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == fmt.Sprintf("ROW-%05d", want) })
				before := terminal.Screen()
				output(20)
				// A changed percentage proves a fresh render happened, while
				// the numbered content proves output did not move the view.
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
					return s.Row(0) != before.Row(0) && strings.TrimSpace(s.Row(1)) == fmt.Sprintf("ROW-%05d", want)
				})
				// The frame callback fences history ingestion; the key/render fence proves
				// the assertion cannot pass merely because the old screen was still shown.
				terminal.Send("j")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == fmt.Sprintf("ROW-%05d", want+1) })
				terminal.Send("k")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == fmt.Sprintf("ROW-%05d", want) })
				after := terminal.Screen()
				for row := 1; row < before.Rows; row++ {
					if before.Row(row) != after.Row(row) {
						t.Fatalf("output shifted viewed row %d: %q -> %q", row, before.Row(row), after.Row(row))
					}
				}
				terminal.Send("G")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("ROW-%05d", count)) })
				bottom := visibleScrollRow(t, terminal.Screen())
				output(10)
				terminal.Send("j")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == fmt.Sprintf("ROW-%05d", bottom+1) })
				terminal.Send("G")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("ROW-%05d", count)) })
				terminal.Send("q")
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
					return !strings.Contains(s.Row(0), "%]") && s.Contains(fmt.Sprintf("ROW-%05d", count))
				})
			})
		}
	}
}
