package ptytest_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRestoreAcknowledgementAfterTabSwitchDoesNotResizeSelectedSessionPTY(t *testing.T) {
	for _, name := range []string{"delayed", "coalesced", "return-before-acks", "return-after-first-ack", "same-size-tabs"} {
		t.Run(name, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			betaCols, betaRows := 95, 28
			if name == "same-size-tabs" {
				betaCols, betaRows = 100, 30
			}
			for _, spec := range []struct {
				id         string
				cols, rows int
			}{{"alpha", 100, 30}, {"beta", betaCols, betaRows}} {
				host := h.StartHost(ptytest.HostOptions{SessionID: spec.id, SessionName: spec.id, Cols: spec.cols, Rows: spec.rows, Shell: restoreShell(t), AllowRemoteResize: true})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				waitRestoreHost(t, recorder, spec.id)
			}
			ready := map[string]chan struct{}{"alpha": make(chan struct{}), "beta": make(chan struct{})}
			viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Restore: true, OnView: func(id string, c *attach.Client) {
				original := c.OnReady
				var once sync.Once
				c.OnReady = func() { original(); once.Do(func() { close(ready[id]) }) }
			}})
			t.Cleanup(viewer.Cancel)
			awaitReady := func(id string) {
				t.Helper()
				select {
				case <-ready[id]:
				case <-time.After(8 * time.Second):
					t.Fatal("view not ready")
				}
			}
			waitRestoreRequest(t, viewer, 100, 30)
			awaitReady("alpha")
			viewer.Send("\x0cn") // Switch before acknowledging alpha's request.
			waitRestoreRequest(t, viewer, betaCols, betaRows)
			awaitReady("beta")
			if name == "same-size-tabs" {
				viewer.DrainRaw()
				viewer.Resize(100, 30) // One acknowledgement for identical requests.
				waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return len(viewer.DrainRaw()) > 0 })
				viewer.Send("same-ack\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:same-ack SIZE:30 100") })
				assertNoRestorePropagation(t, recorder)
				viewer.Resize(88, 26)
				waitGeometrySnapshot(t, recorder, "beta", 88, 26)
				viewer.Send("same-manual-one\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:same-manual-one SIZE:26 88") })
				viewer.Resize(100, 30) // Now this is a user resize, not a stale ack.
				viewer.Send("same-manual-two\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:same-manual-two SIZE:30 100") })
				return
			}
			if name == "return-before-acks" || name == "return-after-first-ack" {
				acks := [][2]int{{100, 30}, {95, 28}, {100, 30}}
				if name == "return-after-first-ack" {
					viewer.DrainRaw()
					viewer.Resize(100, 30)
					waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return len(viewer.DrainRaw()) > 0 })
					viewer.Send("beta-pending\r")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:beta-pending SIZE:28 95") })
					acks = acks[1:]
				}
				viewer.Send("\x0cp")
				waitRestoreRequest(t, viewer, 100, 30)
				for i, size := range acks {
					viewer.DrainRaw()
					viewer.Resize(size[0], size[1])
					waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return len(viewer.DrainRaw()) > 0 })
					key := fmt.Sprintf("return-ack-%d", i)
					viewer.Send(key + "\r")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + key + " SIZE:30 100") })
				}
				assertNoRestorePropagation(t, recorder)
				return
			}
			viewer.DrainRaw()
			if name != "coalesced" {
				viewer.Resize(100, 30)
				// Only resize handling can repaint now: both readiness callbacks
				// completed, output is idle and mock time has not advanced.
				waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return len(viewer.DrainRaw()) > 0 })
				viewer.Send("late-alpha\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:late-alpha SIZE:28 95") })
			}
			viewer.Resize(95, 28)
			waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return len(viewer.DrainRaw()) > 0 })
			viewer.Send("beta-ack\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:beta-ack SIZE:28 95") })
			assertNoRestorePropagation(t, recorder)
			// A real user resize remains accepted after the acknowledgements.
			viewer.Resize(100, 30)
			waitGeometrySnapshot(t, recorder, "beta", 100, 30)
			viewer.Send("manual\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:manual SIZE:30 100") })
		})
	}
}
