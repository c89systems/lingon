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
	"pkt.systems/lingon/internal/terminal"
)

func TestScrollbackCopyPasteCLIAndProcessIsolation(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		t.Run(transport, func(t *testing.T) {
			h := newHarness(t)
			const id = "copy-cli"
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			if transport == "relay" {
				host := h.StartHost(ptytest.HostOptions{SessionID: id, Shell: fixedPromptEmitRowsBash(t), Cols: 100, Rows: 20,
					ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
				t.Cleanup(host.Cancel)
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{id}, 8*time.Second)
			} else {
				cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--session", id, "--cols", "100", "--rows", "20", "--shell", fixedPromptEmitRowsBash(t))
				cmd.Env = testAttachCLIEnv(home)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("start source: %v %s", err, out)
				}
			}
			start := func() *ptytest.PTYSession {
				if transport == "relay" {
					return startLingonAttachCLI(t, h, id, 100, 20)
				}
				return startLingonAttachHeadlessCLI(t, home, id, 100, 20)
			}
			viewer := start()
			t.Cleanup(viewer.Cancel)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
			viewer.Send("emitrows 120\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("ROW-120") && strings.HasSuffix(strings.TrimSpace(s.Row(19)), "PROMPT>")
			})
			viewer.Send("\x0c[gv ")
			middle := (20 - 1) / 2
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return viewer.Cursor().Row == middle+1 && viewer.Snapshot().Cells[middle*100].Mode&terminal.ModeInverse != 0
			})
			first := strings.TrimSpace(viewer.Screen().Row(middle))
			second := strings.TrimSpace(viewer.Screen().Row(middle + 1))
			if !strings.HasPrefix(first, "ROW-") || !strings.HasPrefix(second, "ROW-") {
				t.Fatalf("copy rows %q %q", first, second)
			}
			viewer.Send("j ")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return viewer.Snapshot().Cells[middle*100].Mode&terminal.ModeInverse == 0 && viewer.Cursor().Row == middle+2
			})
			viewer.Send("q\x0c]PASTE_FENCE")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("PASTE_FENCE") && s.Contains(first) && s.Contains(second)
			})
			if viewer.Screen().Contains("command not found") {
				t.Fatal("paste executed Bash lines instead of using bracketed paste")
			}
			viewer.Send("\x03")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.HasSuffix(strings.TrimSpace(s.Row(19)), "PROMPT>") })
			viewer.Send("\x0c\x0c")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.HasPrefix(s.Row(0), "PROMPT>") && !s.Contains("PASTE_FENCE")
			})
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			viewer.Send("\x0c]POPUP_PASTE_FENCE")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				pane := strings.Join(s.Lines[11:], "\n")
				return strings.Contains(pane, first) && strings.Contains(pane, second) && strings.Contains(pane, "POPUP_PASTE_FENCE")
			})
			if viewer.Screen().Contains("command not found") {
				t.Fatal("popup paste executed the copied lines")
			}
			viewer.Send("\x03\x0cj\x0cJ")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return !s.Contains("j hide") && strings.HasPrefix(s.Row(0), "PROMPT>") })
			other := start()
			t.Cleanup(other.Cancel)
			popupScreen(t, other, func(s ptytest.Screen) bool { return strings.HasPrefix(s.Row(0), "PROMPT>") })
			other.Send("\x0c]SECOND_FENCE")
			popupScreen(t, other, func(s ptytest.Screen) bool { return strings.HasPrefix(s.Row(0), "PROMPT>SECOND_FENCE") })
			if other.Screen().Contains(first) || other.Screen().Contains(second) {
				t.Fatal("copied text leaked into another Lingon process")
			}
		})
	}
}
