package ptytest_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestScrollbackFrameDiffNeverRepaintsUnchangedContentPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach", "multi"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			clk := clock.NewMock()
			h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
			shell, emit := controlledScrollOutput(t)
			var awaited atomic.Int64
			received := make(chan int64, 32)
			observe := func(f *protocolpb.Frame) {
				n := awaited.Load()
				if n > 0 && scrollFrameContains(f, fmt.Sprintf("ROW-%05d", n)) {
					select {
					case received <- n:
					default:
					}
				}
			}
			hostObserve := observe
			if mode != "host" {
				hostObserve = nil
			}
			host := h.StartHost(ptytest.HostOptions{SessionID: "diff", SessionName: "diff", Shell: shell, Cols: 60, Rows: 12, OnPublishFrame: hostObserve})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY") })
			waitRestoreHost(t, recorder, "diff")
			terminal := host
			if mode == "attach" {
				terminal = h.StartAttach(ptytest.AttachOptions{SessionID: "diff", Cols: 60, Rows: 12, OnFrame: observe})
			} else if mode == "multi" {
				terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "diff", Cols: 60, Rows: 12, OnView: func(_ string, c *attach.Client) { c.OnFrame = observe }})
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
					case got := <-received:
						if got == int64(count) {
							return
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			for range 5 {
				output(20)
			}
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("ROW-00100") })
			body := strings.TrimSpace(terminal.Screen().Row(1))
			terminal.DrainRaw()
			terminal.Send("\x0c[")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[100%]") })
			raw := terminal.DrainRaw()
			if strings.Contains(raw, body) {
				t.Errorf("entering scrollback repainted unchanged body row %q", body)
			}
			if strings.Contains(raw, "\x1b[2J") {
				t.Error("entering scrollback cleared the screen")
			}
			terminal.Send("g" + strings.Repeat("j", 30))
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == "ROW-00030" })
			before := terminal.Screen()
			terminal.DrainRaw()
			output(20)
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Row(0) != before.Row(0) && strings.TrimSpace(s.Row(1)) == "ROW-00030"
			})
			raw = terminal.DrainRaw()
			if strings.Contains(raw, "ROW-") {
				t.Errorf("historical output repainted unchanged text: %q", raw)
			}
			if strings.Contains(raw, "\x1b[2J") || strings.Contains(raw, "\x1b[2K") {
				t.Error("historical output erased the screen or a line")
			}
			terminal.Send("G")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("ROW-00120") && strings.Contains(s.Row(0), "[100%]") })
			body = strings.TrimSpace(terminal.Screen().Row(1))
			terminal.DrainRaw()
			terminal.Send("q")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return !strings.Contains(s.Row(0), "%]") && s.Contains("ROW-00120") })
			raw = terminal.DrainRaw()
			if strings.Contains(raw, body) {
				t.Errorf("exiting scrollback repainted unchanged body row %q", body)
			}
		})
	}
}

func TestHostSwitchAfterHistoryGrowthUsesSelectedSessionPTY(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "older", SessionName: "older", Shell: searchShell(t), Cols: 60, Rows: 12})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send(strings.Repeat("history\r", 40) + "old-session-fence\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:old-session-fence") })
	host.Send("\x0cc")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("older-2") && s.Contains("READY>") && !s.Contains("old-session-fence")
	})
	host.Send("new-session-fence\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:new-session-fence") })
	host.Send("\x0cp")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:old-session-fence") && !s.Contains("new-session-fence")
	})
	host.Send("\x0cn")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:new-session-fence") && !s.Contains("old-session-fence")
	})
}
