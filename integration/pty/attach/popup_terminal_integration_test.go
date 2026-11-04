//go:build integration

package integrationptyattach_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func popupScreen(t *testing.T, viewer *ptytest.PTYSession, check func(ptytest.Screen) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	if err := viewer.WaitForScreen(ctx, check); err != nil {
		t.Fatalf("popup screen: %v\n%s", err, viewer.Screen().String())
	}
}

func TestPopupTerminalLocalAndRelayCLI(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "local"
		if remote {
			name = "relay"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			const parent = "popup-source"
			args := []string{"-x", "--session", parent, "--cols", "100", "--rows", "20", "--shell", fixedPromptEmitRowsBash(t)}
			if remote {
				args = append(args, "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
			} else {
				args = append(args, "--offline")
			}
			cmd := exec.Command(buildLingonAttachBinary(t), args...)
			cmd.Env = testAttachCLIEnv(home)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("start source: %v %s", err, out)
			}
			var viewer *ptytest.PTYSession
			if remote {
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
				viewer = startLingonAttachCLI(t, h, parent, 100, 20)
			} else {
				viewer = startLingonAttachHeadlessCLI(t, home, parent, 100, 20, "--restore")
			}
			t.Cleanup(viewer.Cancel)
			waitForPromptVisible(t, viewer, 8*time.Second)
			viewer.Send("printf 'PARENT_%s\\n' ORIGINAL\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_ORIGINAL") })
			viewer.DrainRaw()
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			child := strings.Fields(viewer.Screen().Row(10))[0]
			if strings.Contains(viewer.Screen().Row(0), child) {
				t.Fatal("origin's popup appeared as a normal tab")
			}
			viewer.Send("stty size; printf 'POPUP_%s\\n' ALIVE\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("9 100") && s.Contains("POPUP_ALIVE") && s.Contains("PARENT_ORIGINAL")
			})
			viewer.Send("\x0cjstty size; printf 'PARENT_%s\\n' UNRESIZED\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("20 100") && s.Contains("PARENT_UNRESIZED") && !s.Contains("POPUP_ALIVE")
			})
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIVE") && strings.Contains(s.Row(10), child) })
			if strings.Contains(viewer.DrainRaw(), "\x1b[2J") {
				t.Fatal("popup creation or toggle cleared the screen")
			}
			viewer.Resize(90, 30)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), child) })
			viewer.Send("stty size; printf 'POPUP_%s\\n' RESIZED\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("14 90") && s.Contains("POPUP_RESIZED") })
			viewer.Send("\x0cjstty size; printf 'PARENT_%s\\n' RESIZED\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("30 90") && s.Contains("PARENT_RESIZED") })
			viewer.Resize(100, 20)
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(10), child) && s.Contains("POPUP_ALIVE") })
			// A separate viewer sees an ordinary independently attachable session.
			observer := startLingonAttachHeadlessCLI(t, home, child, 100, 9, "--restore")
			t.Cleanup(observer.Cancel)
			popupScreen(t, observer, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIVE") && !s.Contains("j hide") })
			observer.Cancel()
			viewer.Send("\x0cc")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("PROMPT>") && !s.Contains("j hide") && !s.Contains("PARENT_ORIGINAL") && !s.Contains("POPUP_ALIVE")
			})
			viewer.Send("printf 'NORMAL_%s\\n' READY\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("NORMAL_READY") && !s.Contains("Updating session") })
			viewer.Send("\x0cework\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(0), "work") && !s.Contains("Rename session") && !s.Contains("Updating session")
			})
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			second := strings.Fields(viewer.Screen().Row(10))[0]
			if second == child {
				t.Fatal("two parent tabs share a popup")
			}
			viewer.Send("printf 'SECOND_%s\\n' POPUP\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_POPUP") })
			viewer.DrainRaw()
			viewer.Send("\x0cn")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), child) && s.Contains("POPUP_ALIVE") && !s.Contains("SECOND_POPUP")
			})
			viewer.Send("\x0cp")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(10), second) && s.Contains("SECOND_POPUP") })
			if strings.Contains(viewer.DrainRaw(), "\x1b[2J") {
				t.Fatal("switching tabs with popups cleared the screen")
			}
			viewer.Send("\x0ceTools\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "Tools") && !s.Contains("Rename session") && !s.Contains("Updating session")
			})
			if strings.Contains(viewer.Screen().Row(0), "Tools") {
				t.Fatal("renamed popup leaked into the tab bar")
			}
			// Q closes the focused popup while keeping its parent attached.
			viewer.Send("\x0cQ")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return !s.Contains("j hide") && !s.Contains("SECOND_POPUP") && strings.Contains(s.Row(0), "work") && !s.Contains("Updating session")
			})
			viewer.Send("printf 'WORK_%s\\n' SURVIVED\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("WORK_SURVIVED") })
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			viewer.Send("\x0cj\x0cJ")
			// The input echo fences the close acknowledgement and parent focus.
			viewer.Send("printf 'HIDDEN_%s\\n' CLOSED\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return !s.Contains("j hide") && s.Contains("HIDDEN_CLOSED") && !s.Contains("Updating session")
			})
			viewer.Send("\x0c/popup-source\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIVE") && strings.Contains(s.Row(10), child) })
			// Parent termination promotes the popup instead of terminating it.
			viewer.Send("\x0cj\x0cQ")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("POPUP_ALIVE") && strings.Contains(s.Row(0), child) && !s.Contains("j hide")
			})
			viewer.Send("printf 'SURVIVED_%s\\n' PARENT\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SURVIVED_PARENT") })
		})
	}
}
