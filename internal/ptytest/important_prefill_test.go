package ptytest_test

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestImportantPromptDoesNotPrefillMarkedTabPTY(t *testing.T) {
	for _, mode := range []string{"host", "single-attach", "multi-attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "alpha", SessionName: "alpha", Shell: searchShell(t), Cols: 100, Rows: 16})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			host.Send("original\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:original") })
			if mode != "single-attach" {
				host.Send("\x0cc")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool {
					return s.Contains("alpha-2") && s.Contains("READY>") && !s.Contains("REPLY:original")
				})
				host.Send("second\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:second") })
				host.Send("\x0cp")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:original") })
			}
			viewer := host
			if mode != "host" {
				waitRestoreHost(t, recorder, "alpha")
				if mode == "multi-attach" {
					waitRestoreHost(t, recorder, "alpha-2")
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 100, Rows: 16})
				} else {
					viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "alpha", RequestControl: true, Cols: 100, Rows: 16})
				}
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:original") })
			}
			normalFill := viewer.Snapshot().Cells[50].BG
			reply := func(marker string) {
				t.Helper()
				viewer.Send(marker + "\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + marker) })
			}
			viewer.Send("\x0ci")
			searchField(t, viewer, "i alpha")
			viewer.Send("\r")
			reply("marked")
			if mode != "single-attach" && viewer.Snapshot().Cells[50].BG == normalFill {
				t.Fatal("initial Enter did not mark the unmarked active tab")
			}
			viewer.Send("\x0c\x09") // Ctrl+L Ctrl+I uses the same prefill rule.
			searchField(t, viewer, "i")
			viewer.Send("\r")
			reply("empty-keeps")
			if mode != "single-attach" && viewer.Snapshot().Cells[50].BG == normalFill {
				t.Fatal("empty Enter removed the existing mark")
			}
			viewer.Send("\x0ci")
			searchField(t, viewer, "i")
			viewer.Send("\x1b")
			reply("cancel-keeps")
			if mode != "single-attach" && viewer.Snapshot().Cells[50].BG == normalFill {
				t.Fatal("cancelling changed existing marks")
			}
			if mode != "single-attach" {
				viewer.Send("\x0ci")
				searchField(t, viewer, "i")
				viewer.Send("\t")
				searchField(t, viewer, "i alpha-2") // Still cycle from the active tab.
				viewer.Send("\r")
				reply("added-another")
				viewer.Send("\x0cn")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:second") })
				reply("cycle-second")
				viewer.Send("\x0cn")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return s.Contains("REPLY:added-another") && !s.Contains("REPLY:cycle-second")
				})
			}
			viewer.Send("\x0ci")
			searchField(t, viewer, "i")
			viewer.Send("alpha\r") // Removing an existing mark requires naming it explicitly.
			reply("explicit-remove")
			if mode != "single-attach" {
				viewer.Send("\x0cn")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:cycle-second") })
				viewer.Send("\x0ci")
				searchField(t, viewer, "i")
				viewer.Send("alpha-2\r")
				reply("last-removed")
				if viewer.Snapshot().Cells[50].BG != normalFill {
					t.Fatal("explicit name did not remove the last mark")
				}
			}
			viewer.Send("\x0ci")
			want := "i alpha"
			if mode != "single-attach" {
				want = "i alpha-2"
			}
			searchField(t, viewer, want) // Once unmarked, the active tab is prefilled again.
			viewer.Send("\x03")
		})
	}
}
