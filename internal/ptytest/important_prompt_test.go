package ptytest_test

import (
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/ptytest"
)

func TestImportantPromptBatchHostAndAttachPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprint(useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			inactive := importantTabColor(viewer, "alpha-2")
			viewer.Send("\x0ci")
			searchField(t, viewer, "i alpha")
			if importantTabColor(viewer, "alpha-2").BG != inactive.BG {
				t.Fatal("opening prompt changed marks")
			}
			viewer.Send("build.prod.dev \x17")
			searchField(t, viewer, "i alpha") // Whole dotted name, including trailing space.
			viewer.Send("alpha-2 alpha-\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.HasPrefix(strings.TrimSpace(s.Row(11)), "i alpha alpha-2 alpha-")
			})
			if importantTabColor(viewer, "alpha-2").BG != inactive.BG {
				t.Fatal("ambiguous Enter partially toggled marks")
			}
			viewer.Send("\x17alpha-3\raccepted\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:accepted") && s.Contains("REPLY:tab-1") })
			viewer.Send("\x0c/alpha-4\r")
			searchField(t, viewer, "/ alpha-4") // Unmarked tab cannot be opened here.
			viewer.Send("\x1b")
			for i, tab := range []int{2, 3, 1} {
				viewer.Send(fmt.Sprintf("\x0cnnext-%d\r", i))
				if useAttach && tab != 1 {
					settle()
				}
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("REPLY:next-%d", i)) })
				if !viewer.Screen().Contains(fmt.Sprintf("REPLY:tab-%d", tab)) {
					t.Fatalf("batch selected wrong next tab:\n%s", viewer.Screen().String())
				}
			}
			// Tab cycles within an entry; Space accepts it before the next entry.
			viewer.Send("\x0c\x09alpha \t")
			searchField(t, viewer, "i alpha alpha-2")
			viewer.Send("\t")
			searchField(t, viewer, "i alpha alpha-3")
			viewer.Send("\x1b[Z a\t\t\t")
			searchField(t, viewer, "i alpha alpha-2 alpha-3")
			viewer.Send("\rremoved\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:removed") && importantTabColor(viewer, "alpha-2").BG == inactive.BG
			})
			viewer.Send("\x0ci\t\x03cancelled-batch\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:cancelled-batch") })
			if importantTabColor(viewer, "alpha-2").BG != inactive.BG {
				t.Fatal("cancelled batch changed marks")
			}
		})
	}
}

func TestSessionNamesReplaceSpacesHostAndAttachPTY(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "stable-name-id", SessionName: "build server", Shell: searchShell(t), Cols: 80, Rows: 16})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") && strings.Contains(s.Row(0), "build_server") })
	waitRestoreHost(t, recorder, "stable-name-id")
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "stable-name-id", Cols: 80, Rows: 16})
	t.Cleanup(viewer.Cancel)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "build_server") })
	viewer.Send("\x0ceräv build.prod\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "räv_build.prod") })
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "räv_build.prod") })
	viewer.Send("\x0ci")
	searchField(t, viewer, "i räv_build.prod")
	viewer.Send("\x17")
	searchField(t, viewer, "i")
	viewer.Send("\x03identity-unchanged\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:identity-unchanged") })
}

func TestImportantPromptSingleAttachPTY(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "single-important", Shell: searchShell(t), Cols: 100, Rows: 16})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "single-important")
	viewer := h.StartAttach(ptytest.AttachOptions{SessionID: "single-important", RequestControl: true, Cols: 100, Rows: 16})
	t.Cleanup(viewer.Cancel)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	viewer.Send("\x0c/")
	searchField(t, viewer, "/")
	viewer.Send("\x1b\x0ci")
	searchField(t, viewer, "i single-important")
	viewer.Send("\x17bad.name\x17single-important\r\x0c/\t")
	searchField(t, viewer, "/ single-important")
	viewer.Send("\x1b\x0cisingle-important\r\x0c/\t")
	searchField(t, viewer, "/ single-important")
	viewer.Send("\x1bsingle-input\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:single-input") })
}

func TestImportantPromptTabCyclesWithinEntryHostAndAttachPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprint(useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			viewer.Send("\x0c/alpha-3\r")
			settle()
			viewer.Send("origin-entry\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:origin-entry") && s.Contains("REPLY:tab-3") })
			viewer.Send("\x0ci")
			searchField(t, viewer, "i alpha-3")
			viewer.Send("\t")
			searchField(t, viewer, "i alpha-3 alpha-4") // Next tab after the active tab.
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.TrimSpace(s.Row(s.Rows-1)) == "i alpha-3 alpha-4" && viewer.Cursor().Col == len("i alpha-3 alpha-4")+1
			})
			viewer.Send("\t")
			searchField(t, viewer, "i alpha-3 alpha-5")
			viewer.Send("\x1b[Z")
			searchField(t, viewer, "i alpha-3 alpha-4")
			viewer.Send("\t ")
			searchField(t, viewer, "i alpha-3 alpha-5")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.TrimSpace(s.Row(s.Rows-1)) == "i alpha-3 alpha-5" && viewer.Cursor().Col == len("i alpha-3 alpha-5 ")+1
			})
			viewer.Send("alp")
			searchField(t, viewer, "i alpha-3 alpha-5 alpha") // Entered prefix plus gray ghost.
			viewer.Send("\t")
			searchField(t, viewer, "i alpha-3 alpha-5 alpha")
			viewer.Send("\rselected-entry\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:selected-entry") && s.Contains("REPLY:tab-3") })
			viewer.Send("\x0c/alpha-4\r")
			searchField(t, viewer, "/ alpha-4") // Merely previewed tabs were not marked.
			viewer.Send("\x1b\x0cnnext-entry\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:next-entry") && s.Contains("REPLY:tab-5") })
		})
	}
}

func TestImportantSearchScopeBindingsHostAndAttachPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprint(useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			viewer.Send("\x0ci\r\x0c/alpha-2\r")
			searchField(t, viewer, "/ alpha-2") // Slash cannot open an unmarked tab in imode.
			viewer.Send("\x1b\x0c?alpha-2\r")
			if useAttach {
				settle()
			}
			viewer.Send("all-scope\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:all-scope") && s.Contains("REPLY:tab-2") })
			viewer.Send("\x0c/\t")
			searchField(t, viewer, "/ alpha")
			viewer.Send("\rmarked-scope\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:marked-scope") && s.Contains("REPLY:tab-1") })
			viewer.Send("\x0cI\x0c/alpha-3\r")
			if useAttach {
				settle()
			}
			viewer.Send("reset-scope\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:reset-scope") && s.Contains("REPLY:tab-3") })
		})
	}
}
