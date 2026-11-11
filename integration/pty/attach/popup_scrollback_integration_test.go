//go:build integration

package integrationptyattach_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/ptytest"
)

func TestPopupScrollbackKeepsEveryRowBelowSeparatorPTY(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "popup-history")
	script := `#!/bin/sh
stty -echo
printf 'FIRST-PROMPT> '
while IFS= read -r line; do
 printf '\n'
 i=1
 while [ "$i" -le 40 ]; do printf 'POPUP-ROW-%03d\n' "$i"; i=$((i+1)); done
 printf 'LAST-PROMPT> '
done
`
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"host", "relay-attach", "local-attach", "local-restore"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			cfg := filepath.Join(shortHomeDir(t), ".lingon")
			cleanupManagedTerminals(t, cfg)
			var viewer *ptytest.PTYSession
			if strings.HasPrefix(mode, "local-") {
				cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--config-dir", cfg, "--session", "popup-history", "--cols", "100", "--rows", "20", "--shell", shell)
				cmd.Env = testAttachCLIEnv(filepath.Dir(cfg))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("start headless: %v %s", err, output)
				}
				flags := []string{}
				if mode == "local-restore" {
					flags = append(flags, "--restore")
				}
				viewer = startLingonAttachHeadlessCLI(t, filepath.Dir(cfg), "popup-history", 100, 20, flags...)
			} else {
				viewer = h.StartHost(ptytest.HostOptions{SessionID: "popup-history", Shell: shell, Cols: 100, Rows: 20, ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
			}
			t.Cleanup(viewer.Cancel)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FIRST-PROMPT>") })
			if mode == "relay-attach" {
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "popup-history", Cols: 100, Rows: 20})
				t.Cleanup(viewer.Cancel)
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FIRST-PROMPT>") })
			}
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(s.Row(11), "FIRST-PROMPT>")
			})
			viewer.Send("LINES\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(19), "LAST-PROMPT>") })
			viewer.Send("\x0c[g")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(11), "FIRST-PROMPT>") && strings.Contains(s.Row(12), "POPUP-ROW-001")
			})
			if !strings.Contains(viewer.Screen().Row(10), "j hide") {
				t.Fatal("scrollback replaced separator")
			}
			viewer.Send("j")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(11), "POPUP-ROW-001") && strings.Contains(s.Row(19), "POPUP-ROW-009")
			})
			viewer.Send("G")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(11), "POPUP-ROW-033") && strings.Contains(s.Row(19), "LAST-PROMPT>")
			})
			viewer.Send("q")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(11), "POPUP-ROW-033") && strings.Contains(s.Row(19), "LAST-PROMPT>")
			})
		})
	}
}
