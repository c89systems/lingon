package ptytest_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRestoreResendsDisconnectedManualResizeBeforeQueuedInputPTY(t *testing.T) {
	for _, phase := range []string{"before-seed", "after-seed"} {
		t.Run(phase, func(t *testing.T) { testRestorePendingReconnect(t, phase) })
	}
}

func testRestorePendingReconnect(t *testing.T, phase string) {
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "restore-reconnect", Shell: restoreShell(t), Cols: 100, Rows: 30, AllowRemoteResize: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "restore-reconnect")
	gate := netgate.New(clk)
	var reconnecting atomic.Bool
	var views atomic.Int32
	preparing := make(chan struct{})
	release := make(chan struct{})
	ready := make(chan *attach.Client, 4)
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "restore-reconnect", Cols: 80, Rows: 24, Restore: true, Gate: gate,
		BackoffPolicy: backoff.Policy{Base: time.Nanosecond, Max: time.Nanosecond, Factor: 1},
		OnReconnect:   func(string, int) { gate.Allow(); reconnecting.Store(true) },
		TokenRefresher: func(ctx context.Context) (string, error) {
			if reconnecting.CompareAndSwap(true, false) && phase == "before-seed" {
				close(preparing)
				select {
				case <-release:
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			return h.AccessToken(), nil
		},
		OnView: func(_ string, c *attach.Client) {
			if views.Add(1) == 2 && phase == "after-seed" {
				close(preparing)
				select {
				case <-release:
				case <-t.Context().Done():
					return
				}
			}
			original := c.OnReady
			c.OnReady = func() {
				original()
				select {
				case ready <- c:
				case <-t.Context().Done():
				}
			}
		},
	})
	t.Cleanup(viewer.Cancel)
	awaitReady := func() *attach.Client {
		t.Helper()
		select {
		case c := <-ready:
			return c
		case <-time.After(8 * time.Second):
			t.Fatal("view not ready")
			return nil
		}
	}
	first := awaitReady()
	waitRestoreRequest(t, viewer, 100, 30)
	viewer.Resize(100, 30)
	viewer.Send("initial\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:initial SIZE:30 100") })
	first.Close("resize while disconnected")
	select {
	case <-preparing:
	case <-time.After(8 * time.Second):
		t.Fatal("reconnect did not reach construction barrier")
	}
	viewer.Resize(88, 26)
	// Flushing ordinary input drains pending resize events. The following local
	// search overlay fences that flush while connection construction stays blocked.
	viewer.Send("offline\r\x0c/")
	searchField(t, viewer, "/")
	viewer.Send("\x1bx\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return !strings.HasPrefix(s.Row(s.Rows-1), "/ ") })
	viewer.DrainRaw()
	close(release)
	if next := awaitReady(); next == first {
		t.Fatal("reconnect did not replace the client")
	}
	if raw := viewer.DrainRaw(); strings.Contains(raw, "\x1b[8;30;100t") {
		t.Fatal("reconnect restored stale 100x30 instead of pending manual 88x26")
	}
	waitGeometrySnapshot(t, recorder, "restore-reconnect", 88, 26)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:offline SIZE:26 88") })
	viewer.Send("after\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:after SIZE:26 88") })
}
