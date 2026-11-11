//go:build integration

package integrationptyattach_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func TestPopupCtrlDLogsOutThenDetachesCLI(t *testing.T) {
	for _, mode := range []string{"local", "restore", "lingon-x", "lingonx", "relay", "hidden", "promoted"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			const parent = "popup-eof-parent"
			args := []string{"-x", "--session", parent, "--cols", "100", "--rows", "20", "--shell", fixedPromptEmitRowsBash(t)}
			if mode == "relay" {
				args = append(args, "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
			} else {
				args = append(args, "--offline")
			}
			cmd := exec.Command(buildLingonAttachBinary(t), args...)
			cmd.Env = testAttachCLIEnv(home)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("start parent: %v %s", err, out)
			}
			var viewer *ptytest.PTYSession
			switch mode {
			case "relay":
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
				viewer = startLingonAttachCLI(t, h, parent, 100, 20)
			case "restore":
				viewer = startLingonAttachHeadlessCLI(t, home, parent, 100, 20, "--restore")
			case "lingon-x":
				viewer = startLingonRootHeadlessAttachCLI(t, home, parent, 100, 20)
			case "lingonx":
				viewer = startLingonXAttachHeadlessCLI(t, home, parent, 100, 20)
			default:
				viewer = startLingonAttachHeadlessCLI(t, home, parent, 100, 20)
			}
			t.Cleanup(viewer.Cancel)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
			viewer.Send("printf 'PARENT_%s\\n' ALIVE\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_ALIVE") })
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			child := strings.Fields(viewer.Screen().Row(10))[0]
			viewer.Send("printf 'POPUP_%s\\n' ALIVE\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				pane := strings.Join(s.Lines[11:], "\n")
				return strings.Contains(pane, "POPUP_ALIVE") && strings.HasSuffix(strings.TrimSpace(pane), "PROMPT>")
			})
			detachSession := parent
			switch mode {
			case "hidden":
				viewer.Send("\x0cj")
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_ALIVE") && !s.Contains("j hide") })
			case "promoted":
				viewer.Send("\x0cj\x0cQ")
				popupScreen(t, viewer, func(s ptytest.Screen) bool {
					return s.Contains("POPUP_ALIVE") && !s.Contains("j hide") && strings.Contains(s.Row(0), child)
				})
				detachSession = child
			default:
				// Ctrl+D on a nonempty readline must not force-close the popup.
				viewer.Send("printf 'BUFFERED_%s\\n' INPUT\x04\r")
				popupScreen(t, viewer, func(s ptytest.Screen) bool {
					pane := strings.Join(s.Lines[11:], "\n")
					return strings.Contains(pane, "BUFFERED_INPUT") && strings.HasSuffix(strings.TrimSpace(pane), "PROMPT>")
				})
				// At the empty prompt Ctrl+D logs out. Fence the visible focus handoff.
				viewer.Send("\x04")
				popupScreen(t, viewer, func(s ptytest.Screen) bool {
					return s.Contains("PARENT_ALIVE") && !s.Contains("j hide") && !s.Contains("POPUP_ALIVE")
				})
			}
			viewer.Send("printf 'AFTER_%s\\n' POPUP\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("AFTER_POPUP") })
			// Now the ordinary parent is focused, so the second Ctrl+D detaches.
			viewer.Send("\x04")
			if exited, err := viewer.WaitErr(8 * time.Second); !exited || err != nil {
				t.Fatalf("second Ctrl+D did not detach: exited=%v err=%v", exited, err)
			}
			viewer.Cancel()
			observer := startLingonAttachHeadlessCLI(t, home, detachSession, 100, 20)
			t.Cleanup(observer.Cancel)
			popupScreen(t, observer, func(s ptytest.Screen) bool { return s.Contains("AFTER_POPUP") })
			observer.Send("printf 'SURVIVED_%s\\n' DETACH\r")
			popupScreen(t, observer, func(s ptytest.Screen) bool { return s.Contains("SURVIVED_DETACH") })
			if mode == "hidden" {
				// Connecting to that same child as a normal tab has no popup exception.
				independent := startLingonAttachHeadlessCLI(t, home, child, 100, 9, "--restore")
				t.Cleanup(independent.Cancel)
				popupScreen(t, independent, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIVE") })
				independent.Send("\x04")
				if exited, err := independent.WaitErr(8 * time.Second); !exited || err != nil {
					t.Fatalf("ordinary child attachment did not detach: exited=%v err=%v", exited, err)
				}
				independent.Cancel()
				check := startLingonAttachHeadlessCLI(t, home, child, 100, 9, "--restore")
				t.Cleanup(check.Cancel)
				popupScreen(t, check, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIVE") })
			}
		})
	}
}
