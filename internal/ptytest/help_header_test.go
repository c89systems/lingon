package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHelpCopyrightHeaderHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach", "single-attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "help-header", Shell: searchShell(t), Cols: 120, Rows: 50})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			waitRestoreHost(t, recorder, "help-header")
			viewer := host
			if mode == "attach" {
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "help-header", Cols: 120, Rows: 50})
			} else if mode == "single-attach" {
				viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "help-header", Cols: 120, Rows: 50, RequestControl: true})
			}
			t.Cleanup(viewer.Cancel)
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			viewer.Send("\x0ch")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains(mvu.HelpTitle()) && s.Contains("Copyright (C) 2026 C89 Systems AB https://c89.systems") && s.Contains("endpoint:") && s.Contains("press q or Q to close help")
			})
			screen := viewer.Screen().String()
			if strings.Contains(screen, "session: ") || strings.Contains(screen, mvu.HelpTitle()+" controls") {
				t.Fatal("obsolete help header remains:\n" + screen)
			}
			pasteEntry := false
			for _, row := range viewer.Screen().Lines {
				line := strings.TrimSpace(row)
				if strings.HasPrefix(line, "Ctrl+L ]  ") {
					pasteEntry = true
				}
				if strings.HasPrefix(line, "Ctrl+L [  ") && strings.Contains(line, "Ctrl+L ]") {
					t.Fatal("live-terminal paste is incorrectly described as a scrollback command")
				}
			}
			if !pasteEntry {
				t.Fatal("help has no standalone Ctrl+L ] paste entry")
			}
			viewer.Send("qheader-fence\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:header-fence") && !s.Contains(mvu.HelpCopyright) })
		})
	}
}
