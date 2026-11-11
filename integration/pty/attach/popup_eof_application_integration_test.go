//go:build integration

package integrationptyattach_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestPopupCtrlDRemainsApplicationInputPTY(t *testing.T) {
	for _, mode := range []string{"host-raw", "attach-raw", "attach-canonical"} {
		t.Run(mode, func(t *testing.T) {
			canonical := mode == "attach-canonical"
			script := "#!/bin/sh\nstty raw -echo\nprintf '\\r\\nAPP_READY\\r\\n'\nod -An -tx1 -N1\nprintf '\\r\\nAPP_RECEIVED\\r\\n'\nIFS= read -r ignored\n"
			if canonical {
				script = "#!/bin/sh\nprintf '\\nAPP_READY\\n'\nwhile IFS= read -r line; do printf '\\nREPLY:%s\\n' \"$line\"; done\n"
			}
			shell := filepath.Join(t.TempDir(), "application")
			if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			h := newHarness(t, ptytest.WithClock(clock.NewMock()))
			cfg := shortHomeDir(t)
			cleanupManagedTerminals(t, cfg)
			const parent = "popup-application-parent"
			host := h.StartHost(ptytest.HostOptions{SessionID: parent, Shell: shell, Cols: 100, Rows: 20,
				ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
			t.Cleanup(host.Cancel)
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("APP_READY") })
			viewer := host
			if mode != "host-raw" {
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: parent, Cols: 100, Rows: 20})
				t.Cleanup(viewer.Cancel)
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("APP_READY") })
			}
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "APP_READY")
			})
			viewer.Send("\x04")
			if canonical {
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return !s.Contains("j hide") && s.Contains("APP_READY") })
				viewer.Send("parent-survived\r")
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-survived") })
			} else {
				popupScreen(t, viewer, func(s ptytest.Screen) bool {
					pane := strings.Join(s.Lines[11:], "\n")
					return s.Contains("j hide") && strings.Contains(pane, " 04") && strings.Contains(pane, "APP_RECEIVED")
				})
			}
		})
	}
}
