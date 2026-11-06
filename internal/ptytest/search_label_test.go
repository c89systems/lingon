package ptytest_test

import (
	"fmt"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestSearchPromptUsesInvokingKeyPTY(t *testing.T) {
	for _, mode := range []string{"host", "single-attach", "multi-attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "alpha", SessionName: "alpha", Shell: searchShell(t), Cols: 80, Rows: 16})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			viewer := host
			if mode != "host" {
				waitRestoreHost(t, recorder, "alpha")
				if mode == "single-attach" {
					viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "alpha", RequestControl: true, Cols: 80, Rows: 16})
				} else {
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 16})
				}
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			}
			for _, important := range []bool{false, true} {
				for _, key := range []string{"?", "/"} {
					viewer.Send("\x0c" + key)
					searchField(t, viewer, key)
					viewer.Send("al")
					searchField(t, viewer, key+" alpha")
					waitSearchScreen(t, viewer, func(_ ptytest.Screen) bool { return viewer.Cursor().Col == 5 })
					viewer.Send("\t")
					searchField(t, viewer, key+" alpha")
					marker := fmt.Sprintf("cancel-%v-%s", important, key)
					viewer.Send("\x1b" + marker + "\r")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + marker) })
				}
				if !important {
					viewer.Send("\x0ci")
					searchField(t, viewer, "i alpha")
					viewer.Send("\rmarked\r")
					waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:marked") })
				}
			}
		})
	}
}
