package ptytest_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func scrollbackCopyFixture(t *testing.T, bracketed bool) (string, func(int), func()) {
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
	flag := filepath.Join(dir, "sink")
	count := 24
	mode := ""
	if bracketed {
		count += 12
		mode = "\\033[?2004h"
	}
	script := fmt.Sprintf(`#!/bin/sh
stty -echo
if [ -f '%s' ]; then
 stty raw -echo
 printf '%s\r\nSINK_READY\r\nHEX_BEGIN\r\n'
 od -v -An -tx1 -N%d
 printf '\r\nHEX_END\r\n'
 while IFS= read -r ignored; do :; done
else
 printf '\nREADY\n'
 i=0
 while IFS= read -r count; do
  end=$((i+count))
  while [ $i -lt $end ]; do i=$((i+1)); printf '\033[31mROW-%%03d\033[0m\n' "$i"; done
 done < '%s'
fi
`, flag, mode, count, fifo)
	shell := filepath.Join(dir, "shell")
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return shell, func(n int) {
			t.Helper()
			if _, err := fmt.Fprintln(pipe, n); err != nil {
				t.Fatal(err)
			}
		}, func() {
			t.Helper()
			if err := os.WriteFile(flag, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
}

func TestScrollbackVisualNavigationAndResizePTY(t *testing.T) {
	for _, mode := range []string{"host", "single-attach", "attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			shell, emit := controlledScrollOutput(t)
			host := h.StartHost(ptytest.HostOptions{SessionID: "copy-navigation", Shell: shell, Cols: 100, Rows: 20})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY") })
			emit(200)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ROW-00200") })
			waitRestoreHost(t, recorder, "copy-navigation")
			viewer := host
			if mode == "single-attach" {
				viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "copy-navigation", Cols: 100, Rows: 20, RequestControl: true})
			}
			if mode == "attach" {
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "copy-navigation", Cols: 100, Rows: 20})
			}
			t.Cleanup(viewer.Cancel)
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-00200") })
			viewer.Send("\x0c[v")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return viewer.Cursor().Row == 10 && viewer.Cursor().Col == 1 })
			rowNumber := func(s ptytest.Screen) int {
				fields := strings.Fields(s.Row(viewer.Cursor().Row - 1))
				if len(fields) == 0 || !strings.HasPrefix(fields[0], "ROW-") {
					return -1
				}
				n, _ := strconv.Atoi(strings.TrimPrefix(fields[0], "ROW-"))
				return n
			}
			want := rowNumber(viewer.Screen())
			if want <= 0 {
				t.Fatal("missing initial numbered cursor row")
			}
			for _, move := range []struct {
				keys  string
				delta int
			}{{"k", -1}, {"j", 1}, {"K", -5}, {"J", 5}, {"u", -10}, {"d", 10}, {"\x1b[5~", -10}, {"\x1b[6~", 10}, {"w", -10}, {"s", 10}, {"\x1b[A", -1}, {"\x1b[B", 1}, {"\x1b[<64;2;4M", -3}, {"\x1b[<65;2;4M", 3}} {
				want += move.delta
				viewer.Send(move.keys)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return rowNumber(s) == want && viewer.Cursor().Col == 1 })
			}
			// Horizontal input and reserved uppercase V cannot move the cursor.
			want++
			viewer.Send("hHlL\x1b[C\x1b[DVj")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return rowNumber(s) == want && viewer.Cursor().Col == 1 })
			viewer.Send(" j")
			want++
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return rowNumber(s) == want && viewer.Snapshot().Cells[(viewer.Cursor().Row-1)*100].Mode&terminal.ModeInverse != 0
			})
			viewer.Resize(80, 16)
			viewer.Send("k")
			want--
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Cols == 80 && s.Rows == 16 && rowNumber(s) == want && viewer.Cursor().Col == 1 && viewer.Snapshot().Cells[(viewer.Cursor().Row-1)*80].Mode&terminal.ModeInverse != 0
			})
			for _, jump := range []struct {
				keys, hint string
				row        int
			}{{"g", "[0%]", 1}, {"G", "[100%]", 16}, {"\x1b[H", "[0%]", 1}, {"\x1b[F", "[100%]", 16}, {"\x1b[1~", "[0%]", 1}, {"\x1b[4~", "[100%]", 16}} {
				viewer.Send(jump.keys)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return viewer.Cursor().Row == jump.row && strings.Contains(s.Row(0), jump.hint)
				})
			}
			viewer.Send("v")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return viewer.Cursor().Row == 1 && strings.Contains(s.Row(0), "%]") })
			viewer.Send("qLEAK_CHECK\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("LEAK_CHECK") && !strings.Contains(s.Row(0), "%]") })
		})
	}
}

func pastedHex(t *testing.T, viewer *ptytest.PTYSession) []byte {
	t.Helper()
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("HEX_END") })
	text := viewer.Screen().String()
	_, text, ok := strings.Cut(text, "HEX_BEGIN")
	if !ok {
		t.Fatal("missing HEX_BEGIN")
	}
	text, _, ok = strings.Cut(text, "HEX_END")
	if !ok {
		t.Fatal("missing HEX_END")
	}
	got, err := hex.DecodeString(strings.Join(strings.Fields(text), ""))
	if err != nil {
		t.Fatalf("hex output %q: %v", text, err)
	}
	return got
}

func TestScrollbackVisualLineCopyPasteAcrossTabsPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach", "remote-host"} {
		for _, bracketed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bracketed=%v", mode, bracketed), func(t *testing.T) {
				clk := clock.NewMock()
				recorder := ptytest.NewWSRecorder()
				h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
				shell, emit, sink := scrollbackCopyFixture(t, bracketed)
				host := h.StartHost(ptytest.HostOptions{SessionID: "copy-tabs", Shell: shell, Cols: 100, Rows: 20})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY") })
				emit(120)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ROW-120") })
				sink()
				host.Send("\x0cc")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("SINK_READY") })
				host.Send("\x0cn")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ROW-120") && !s.Contains("SINK_READY") })
				waitRestoreHost(t, recorder, "copy-tabs")
				waitRestoreHost(t, recorder, "copy-tabs-2")
				viewer := host
				if mode == "attach" {
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "copy-tabs", Cols: 100, Rows: 20})
					t.Cleanup(viewer.Cancel)
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-120") })
				}
				if mode == "remote-host" {
					viewer = h.StartHost(ptytest.HostOptions{SessionID: "z-copy-owner", Shell: searchShell(t), Cols: 100, Rows: 20})
					t.Cleanup(viewer.Cancel)
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
						return strings.Contains(s.Row(0), "copy-tabs") && strings.Contains(s.Row(0), "copy-tabs-2")
					})
					viewer.Send("\x0c/copy-tabs\r")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-120") })
				}
				viewer.Send("\x0c[gs")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return strings.Contains(s.Row(0), "%]") && strings.HasPrefix(strings.TrimSpace(s.Row(1)), "ROW-")
				})
				viewer.Send("v")
				middle := (20 - 1) / 2
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return viewer.Cursor().Row == middle+1 && viewer.Cursor().Col == 1 })
				before := viewer.Screen()
				lines := []string{strings.TrimSpace(before.Row(middle)), strings.TrimSpace(before.Row(middle + 1)), strings.TrimSpace(before.Row(middle + 2))}
				for _, line := range lines {
					if len(line) != 7 || !strings.HasPrefix(line, "ROW-") {
						t.Fatalf("unexpected selection line %q", line)
					}
				}
				viewer.Send(" jj")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					snap := viewer.Snapshot()
					return viewer.Cursor().Row == middle+3 && snap.Cells[middle*100].Mode&terminal.ModeInverse != 0 && snap.Cells[(middle+2)*100+99].Mode&terminal.ModeInverse != 0
				})
				// Output continues, while selected row identities and cursor stay fixed.
				emit(50)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return s.Row(0) != before.Row(0) && strings.TrimSpace(s.Row(middle)) == lines[0] && viewer.Cursor().Row == middle+3
				})
				viewer.Send(" ")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return viewer.Snapshot().Cells[middle*100].Mode&terminal.ModeInverse == 0 && viewer.Cursor().Row == middle+3
				})
				// An uncommitted replacement selection must not replace the clipboard.
				if bracketed {
					viewer.Send(" jv")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
						return viewer.Cursor().Row == 1 && viewer.Snapshot().Cells[middle*100].Mode&terminal.ModeInverse == 0
					})
					viewer.Send("q")
				} else {
					viewer.Send(" jq")
				}
				viewer.Send("\x0cn")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SINK_READY") })
				viewer.Send("\x0c]")
				want := strings.Join(lines, "\n") + "\n"
				if bracketed {
					want = "\x1b[200~" + want + "\x1b[201~"
				} else {
					want = strings.ReplaceAll(want, "\n", "\r")
				}
				if got := string(pastedHex(t, viewer)); got != want {
					t.Fatalf("paste bytes %q, want %q", got, want)
				}
				// A separate UI instance starts with an empty clipboard, even when
				// attached to terminals managed by this same backend.
				host.Send("\x0cc")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("SINK_READY") && !s.Contains("HEX_END") })
				waitRestoreHost(t, recorder, "copy-tabs-3")
				other := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "copy-tabs-3", Cols: 100, Rows: 20})
				t.Cleanup(other.Cancel)
				waitSearchScreen(t, other, func(s ptytest.Screen) bool { return s.Contains("SINK_READY") })
				filler := strings.Repeat("A", len(want))
				other.Send("\x0c]" + filler)
				if got := string(pastedHex(t, other)); got != filler {
					t.Fatalf("clipboard leaked to another UI instance: %q", got)
				}
				for _, record := range recorder.Frames() {
					var f protocolpb.Frame
					if proto.Unmarshal(record.Raw, &f) == nil && f.GetClipboard() != nil {
						t.Fatal("text copy/paste used the cross-process image clipboard protocol")
					}
				}
			})
		}
	}
}
