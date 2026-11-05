package ptytest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func importantTabColor(viewer *ptytest.PTYSession, name string) terminal.Cell {
	x := strings.Index(viewer.Screen().Row(0), name)
	if x < 0 {
		return terminal.Cell{}
	}
	return viewer.Snapshot().Cells[x]
}

func sameTabColor(a, b terminal.Cell) bool { return a.FG == b.FG && a.BG == b.BG && a.Mode == b.Mode }

func TestImportantTabsHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			clk := clock.NewMock()
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "important", SessionName: "alpha", Shell: searchShell(t), Cols: 100, Rows: 20})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			host.Send("tab-1\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
			for i := 2; i <= 3; i++ {
				host.Send("\x0cc")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool {
					return s.Contains("READY>") && !s.Contains(fmt.Sprintf("REPLY:tab-%d", i-1)) && strings.Contains(s.Row(0), fmt.Sprintf("alpha-%d", i))
				})
				host.Send(fmt.Sprintf("tab-%d\r", i))
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("REPLY:tab-%d", i)) })
			}
			host.Send("\x0cn")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
			viewer := host
			ready := make(chan *attach.Client, 8)
			settle := func() {
				t.Helper()
				if mode == "attach" {
					select {
					case <-ready:
					case <-time.After(8 * time.Second):
						t.Fatal("view readiness barrier")
					}
				}
				clk.Add(4 * time.Second)
			}
			if mode == "attach" {
				for _, id := range []string{"important", "alpha-2", "alpha-3"} {
					waitRestoreHost(t, recorder, id)
				}
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "important", Cols: 100, Rows: 20, OnView: func(_ string, c *attach.Client) {
					original := c.OnReady
					c.OnReady = func() { original(); ready <- c }
				}})
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
			}
			settle()
			viewer.Send("initial-fence\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:initial-fence") && !strings.Contains(s.Row(0), "connected")
			})
			active, inactive := importantTabColor(viewer, "alpha"), importantTabColor(viewer, "alpha-3")
			baseFill := viewer.Snapshot().Cells[99].BG
			viewer.DrainRaw()
			viewer.Send("\x0ci\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return sameTabColor(importantTabColor(viewer, "alpha"), active) && importantTabColor(viewer, "alpha-3").BG != inactive.BG && viewer.Snapshot().Cells[99].BG != baseFill
			})
			if strings.Contains(viewer.DrainRaw(), "\x1b[2J") {
				t.Fatal("mark cleared the screen")
			}
			// The original host's UI does not inherit marks from an attach UI.
			if mode == "attach" && !sameTabColor(importantTabColor(host, "alpha-3"), inactive) {
				t.Fatal("marks leaked into the host UI")
			}
			viewer.Send("\x0c/alpha-2\r")
			settle()
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:tab-2") && importantTabColor(viewer, "alpha-2").BG != active.BG
			})
			// The local search field fences the complete frame, without advancing
			// time or depending on more application output to reveal focus.
			viewer.Send("\x0c/")
			searchField(t, viewer, "/")
			focused := importantTabColor(viewer, "alpha-2")
			unfocused := importantTabColor(viewer, "alpha-3")
			focusMode := terminal.ModeBold | terminal.ModeUnderline
			if focused.Mode&focusMode != focusMode || unfocused.Mode&focusMode != 0 {
				t.Fatalf("active unimportant tab has no distinct focus cue: active=%+v inactive=%+v", focused, unfocused)
			}
			if focused.BG == unfocused.BG {
				t.Fatal("active unimportant tab uses the inactive background")
			}
			viewer.Send("\x1b\x0c\x09\r") // Cancel search, Ctrl+L Ctrl+I.
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return sameTabColor(importantTabColor(viewer, "alpha-2"), active) })
			viewer.Send("\x0c?\t") // Next important tab from alpha-2.
			searchField(t, viewer, "/ alpha")
			viewer.Send("\t")
			searchField(t, viewer, "/ alpha-2")
			viewer.Send("\x1b[Z")
			searchField(t, viewer, "/ alpha")
			viewer.Send("\x1bcancelled\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:cancelled") && s.Contains("REPLY:tab-2") })
			// Ordinary search includes unmarked tabs and preserves ghost completion.
			viewer.Send("\x0c/alpha-3\r")
			settle()
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-3") })
			viewer.Send("\x0c?alpha-3\r")
			searchField(t, viewer, "/ alpha-3")
			if strings.Contains(viewer.Screen().Row(0), "[") {
				t.Fatal("unmarked exact name became a candidate")
			}
			viewer.Send("\x1b\x0c?\t")
			searchField(t, viewer, "/ alpha")
			viewer.Send("\x1b\x0c?\x1b[Z")
			searchField(t, viewer, "/ alpha-2")
			viewer.Send("\x1b\x0cI\x0c?\t")
			searchField(t, viewer, "/ alpha")
			if !sameTabColor(importantTabColor(viewer, "alpha"), inactive) {
				t.Fatal("reset did not restore inactive colors")
			}
			viewer.Send("\x1breset-fence\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:reset-fence") && sameTabColor(importantTabColor(viewer, "alpha-3"), active) && viewer.Snapshot().Cells[99].BG == baseFill
			})
			if mode != "host" {
				return // Remote popup creation is covered by the real CLI matrix.
			}
			// Marking while a popup has input focus marks its parent tab.
			viewer.Send("\x0cj")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
			})
			viewer.Send("\x0ci\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return sameTabColor(importantTabColor(viewer, "alpha-3"), active) && importantTabColor(viewer, "alpha").BG != inactive.BG
			})
			viewer.Send("\x0cj\x0c?\t")
			searchField(t, viewer, "/ alpha-3")
			viewer.Send("\x1b\x0cI")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return sameTabColor(importantTabColor(viewer, "alpha"), inactive) })
		})
	}
}

func TestImportantTabToggleAndNavigationPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprint(useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			viewer.Send("\x0ci\r\x0c/alpha-4\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-4") })
			viewer.Send("\x0ci\r\x0c/alpha-3\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-3") })
			for i, step := range []struct {
				keys string
				tab  int
			}{
				{"\x0cn", 4}, {"\x0cp", 1}, {"\x0c\x0e", 4}, {"\x0c\x10", 1},
			} {
				fence := fmt.Sprintf("cycle-fence-%d", i)
				viewer.DrainRaw()
				viewer.Send(step.keys + fence + "\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + fence) })
				if !viewer.Screen().Contains(fmt.Sprintf("REPLY:tab-%d", step.tab)) {
					t.Fatalf("%q selected wrong tab:\n%s", step.keys, viewer.Screen().String())
				}
				if strings.Contains(viewer.DrainRaw(), "\x1b[2J") {
					t.Fatal("restricted cycling cleared the screen")
				}
			}
			// Unmark alpha, leaving alpha-4 as the sole important tab.
			viewer.Send("\x0ci\r\x0cnone-important\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:one-important") })
			if !viewer.Screen().Contains("REPLY:tab-4") {
				t.Fatal("toggle failed to remove alpha from cycling")
			}
			viewer.Send("\x0cpstill-four\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:still-four") && s.Contains("REPLY:one-important")
			})
			// Removing the last mark restores normal cycling automatically.
			viewer.Send("\x0c\x09\r\x0cnnormal-next\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:normal-next") })
			if !viewer.Screen().Contains("REPLY:tab-5") {
				t.Fatal("last toggle did not leave important mode")
			}
			viewer.Send("\x0ci\r\x0cI\x0cnreset-next\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:reset-next") && s.Contains("REPLY:tab-6") })
		})
	}
}

func TestImportantSearchScrollsOverflowTabsPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprint(useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			viewer.Send("\x0ci\r\x0c/alpha-6\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-6") })
			viewer.Send("\x0ci\r\x0c?\t")
			searchField(t, viewer, "/ alpha")
			viewer.DrainRaw()
			viewer.Send("\t")
			searchField(t, viewer, "/ alpha-6")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(0), "alpha-6]") && s.Contains("REPLY:tab-6")
			})
			if strings.Contains(viewer.DrainRaw(), "\x1b[2J") {
				t.Fatal("important search cleared the screen")
			}
			viewer.Send("\x1b[Z")
			searchField(t, viewer, "/ alpha")
			viewer.Send("\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
		})
	}
}

func TestImportantTabsSurviveRenameAndReconnectPTY(t *testing.T) {
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	for _, spec := range []struct{ id, name string }{{"important-reconnect", "alpha"}, {"unmarked", "beta"}} {
		host := h.StartHost(ptytest.HostOptions{SessionID: spec.id, SessionName: spec.name, Shell: searchShell(t), Cols: 100, Rows: 20})
		t.Cleanup(host.Cancel)
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
		waitRestoreHost(t, recorder, spec.id)
	}
	gate := netgate.New(clk)
	ready := make(chan *attach.Client, 8)
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "important-reconnect", Cols: 100, Rows: 20, Gate: gate,
		BackoffPolicy: backoff.Policy{Base: time.Nanosecond, Max: time.Nanosecond, Factor: 1},
		OnReconnect:   func(string, int) { gate.Allow() },
		OnView: func(_ string, c *attach.Client) {
			original := c.OnReady
			c.OnReady = func() { original(); ready <- c }
		},
	})
	t.Cleanup(viewer.Cancel)
	await := func() *attach.Client {
		t.Helper()
		select {
		case c := <-ready:
			return c
		case <-time.After(8 * time.Second):
			t.Fatal("reconnected view not ready")
			return nil
		}
	}
	first := await()
	viewer.Send("\x0ci\r\x0cerenamed\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(0), "renamed") && !s.Contains("Rename session")
	})
	viewer.Send("\x0c?ren")
	searchField(t, viewer, "/ renamed")
	viewer.Send("\x1bbefore-reconnect\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:before-reconnect") })
	first.Close("important tab reconnect proof")
	if next := await(); next == first {
		t.Fatal("reconnect did not replace the client")
	}
	viewer.Send("\x0c?beta\r")
	searchField(t, viewer, "/ beta")
	if strings.Contains(viewer.Screen().Row(0), "[") {
		t.Fatal("reconnect lost important-only filtering")
	}
	viewer.Send("\x1b\x0c?\t")
	searchField(t, viewer, "/ renamed")
	viewer.Send("\rpost-reconnect\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:post-reconnect") })
	viewer.Send("\x0cI\x0c?\t")
	searchField(t, viewer, "/ beta")
}
