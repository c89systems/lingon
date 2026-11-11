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

func TestPopupReactivationFollowsCurrentWindowCLI(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		t.Run(transport, func(t *testing.T) {
			h := newHarness(t)
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			for _, id := range []string{"alpha", "beta"} {
				args := []string{"-x", "--session", id, "--cols", "80", "--rows", "20", "--shell", fixedPromptEmitRowsBash(t)}
				if transport == "relay" {
					args = append(args, "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
				} else {
					args = append(args, "--offline")
				}
				cmd := exec.Command(buildLingonAttachBinary(t), args...)
				cmd.Env = testAttachCLIEnv(home)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("start %s: %v %s", id, err, out)
				}
			}
			var viewer *ptytest.PTYSession
			if transport == "relay" {
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"alpha", "beta"}, 8*time.Second)
				viewer = startLingonAttachCLI(t, h, "alpha", 80, 20)
			} else {
				viewer = startLingonAttachHeadlessCLI(t, home, "alpha", 80, 20)
			}
			t.Cleanup(viewer.Cancel)
			waitForPromptVisible(t, viewer, 8*time.Second)
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			child := strings.Fields(viewer.Screen().Row(10))[0]
			viewer.Send("printf 'INITIAL=%s\\n' \"$(stty size)\"\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("INITIAL=9 80") })
			viewer.Send("\x0cn")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return !s.Contains("j hide") && s.Contains("PROMPT>") })
			viewer.Resize(90, 30)
			viewer.Send("printf 'BETA=%s\\n' \"$(stty size)\"\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("BETA=30 90") })
			viewer.Send("\x0cp")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), child) })
			viewer.Send("printf 'RETURNED_%s=%s\\n' SIZE \"$(stty size)\"\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("RETURNED_SIZE=") })
			// Inspect the actual child reply, without waiting for it to become correct.
			if !viewer.Screen().Contains("RETURNED_SIZE=14 90") {
				t.Fatalf("reactivated popup uses stale geometry:\n%s", viewer.Screen().String())
			}
			// Ensure the current popup remains independently usable after the resize.
			viewer.Send("printf 'FOCUS_%s\\n' OK\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FOCUS_OK") })
		})
	}
}
