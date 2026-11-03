package ptytest_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestJoiningViewerPreservesScrollbackAnchorPTY(t *testing.T) {
	for _, position := range []string{"newest", "historical"} {
		t.Run(position, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			shell, emit := controlledScrollOutput(t)
			host := h.StartHost(ptytest.HostOptions{SessionID: "replay", SessionName: "replay", Shell: shell, Cols: 60, Rows: 12})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY") })
			waitRestoreHost(t, recorder, "replay")
			emit(300) // The publisher must replay more than two 100-row chunks.
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ROW-00300") })
			var client atomic.Pointer[attach.Client]
			processed := make(chan struct{}, 1)
			viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "replay", Cols: 60, Rows: 12, OnView: func(_ string, c *attach.Client) {
				client.Store(c)
				c.OnFrame = func(f *protocolpb.Frame) {
					if scrollFrameContains(f, "ROW-00301") {
						select {
						case processed <- struct{}{}:
						default:
						}
					}
				}
			}})
			t.Cleanup(viewer.Cancel)
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-00300") })
			viewer.Send("\x0c[")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[100%]") })
			if position == "historical" {
				viewer.Send("g" + strings.Repeat("j", 50))
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == "ROW-00050" })
			}
			before := strings.TrimSpace(viewer.Screen().Row(1))
			viewer.DrainRaw()
			other := h.StartAttach(ptytest.AttachOptions{SessionID: "replay", Cols: 60, Rows: 12})
			t.Cleanup(other.Cancel)
			waitSearchScreen(t, other, func(s ptytest.Screen) bool { return s.Contains("ROW-00300") })
			// A subsequent output frame is an ordered read-loop barrier: every
			// replacement chunk and its snapshot preceded this frame on the wire.
			emit(1)
			select {
			case <-processed:
			case <-time.After(8 * time.Second):
				t.Fatal("post-replacement frame was not read")
			}
			client.Load().RenderCurrent()
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(1)) == before })
			if raw := viewer.DrainRaw(); strings.Contains(raw, before) || strings.Contains(raw, "\x1b[2J") {
				t.Fatalf("replacement repainted anchored history: %q", raw)
			}
		})
	}
}
