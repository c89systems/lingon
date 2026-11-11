//go:build integration && linux

package integrationptyattach_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func TestApplicationEOFNativeTermiosAndPartialLineCLI(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		for _, popup := range []bool{false, true} {
			name := transport + "/normal"
			if popup {
				name = transport + "/popup"
			}
			t.Run(name, func(t *testing.T) {
				shell := filepath.Join(t.TempDir(), "stdin-application")
				const script = `#!/bin/sh
stty -echo icanon
printf '\nSHELL_READY\n'
while IFS= read -r command; do
 case "$command" in
  app)
   settings=$(stty -a)
   case "$settings" in
    *'eof = ^D;'*) printf '\nEOF_NATIVE\n' ;;
    *) printf '\nEOF_CHANGED: %s\n' "$settings" ;;
   esac
   printf 'APP_READING\n'
   cat
   printf '\nAPP_EOF\n'
   ;;
  fence) printf '\nSHELL_SURVIVED\n' ;;
 esac
done
`
				if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				h := newHarness(t)
				home := shortHomeDir(t)
				cfg := filepath.Join(home, lingon.DefaultConfigDirName)
				cleanupManagedTerminals(t, cfg)
				const id = "application-eof"
				if transport == "relay" {
					host := h.StartHost(ptytest.HostOptions{SessionID: id, Shell: shell, Cols: 100, Rows: 30,
						ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
					t.Cleanup(host.Cancel)
					waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{id}, 8*time.Second)
				} else {
					cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--session", id, "--cols", "100", "--rows", "30", "--shell", shell)
					cmd.Env = testAttachCLIEnv(home)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("start source: %v %s", err, out)
					}
				}
				var viewer *ptytest.PTYSession
				if transport == "relay" {
					viewer = startLingonAttachCLI(t, h, id, 100, 30)
				} else {
					viewer = startLingonAttachHeadlessCLI(t, home, id, 100, 30)
				}
				t.Cleanup(viewer.Cancel)
				wait := func(check func(ptytest.Screen) bool) {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
					defer cancel()
					if err := viewer.WaitForScreen(ctx, check); err != nil {
						t.Fatalf("screen: %v\n%s", err, viewer.Screen().String())
					}
				}
				wait(func(s ptytest.Screen) bool { return s.Contains("SHELL_READY") })
				if popup {
					viewer.Send("\x0cj")
					wait(func(s ptytest.Screen) bool {
						return strings.Contains(s.Row(15), "j hide") && strings.Contains(strings.Join(s.Lines[16:], "\n"), "SHELL_READY")
					})
				}
				viewer.Send("app\r")
				wait(func(s ptytest.Screen) bool { return s.Contains("APP_READING") })
				// This positive application report fails deterministically before the
				// fix, without trying to win the asynchronous PTY input/termios race.
				if !viewer.Screen().Contains("EOF_NATIVE") {
					t.Fatal("remote control rewrote the application's EOF key:\n" + viewer.Screen().String())
				}
				key := "\x0cd"
				if popup {
					key = "\x04"
				}
				viewer.Send("partial-line" + key)
				wait(func(s ptytest.Screen) bool { return s.Contains("partial-line") })
				if viewer.Screen().Contains("APP_EOF") {
					t.Fatal("Ctrl+D closed stdin instead of flushing the partial line")
				}
				viewer.Send(key + "fence\r")
				wait(func(s ptytest.Screen) bool { return s.Contains("APP_EOF") && s.Contains("SHELL_SURVIVED") })
				if viewer.Screen().Contains("^D") {
					t.Fatal("explicit EOF was echoed as literal control data")
				}
			})
		}
	}
}
