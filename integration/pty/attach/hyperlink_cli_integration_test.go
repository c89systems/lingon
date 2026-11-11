//go:build integration

package integrationptyattach_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestOSC8HyperlinksLingonxLocalAndRelayCLI(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		t.Run(transport, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := newHarness(t, ptytest.WithWSRecorder(recorder))
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			shell := filepath.Join(t.TempDir(), "hyperlink-shell")
			script := `#!/bin/sh
printf '\nOSC8_READY\n'
while IFS= read -r line; do
 if [ "$line" = history ]; then
  printf '\r\n\033]8;id=history;https://example.invalid/history\033\\SCROLL_LINK\033]8;;\033\\\r\n'
  n=1; while [ "$n" -le 35 ]; do printf 'HISTORY-%02d\r\n' "$n"; n=$((n+1)); done
  printf 'CLI_DONE:history\n'
 else
  uri='https://example.invalid/one'; [ "$line" = other ] && uri='https://example.invalid/two'
  printf '\033[4;1H\033]8;id=cli;%s\033\\\033[4;34mChalmers Ventures\033[0m\033]8;;\033\\\033[K\033[6;1HCLI_DONE:%s\033[K' "$uri" "$line"
 fi
done
`
			if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"--session", "osc8-cli", "-g", "80x16", "--shell", shell, "--disable-desktop-notifications"}
			if transport == "local" {
				args = append(args, "--offline")
			} else {
				args = append(args, "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
			}
			source := exec.Command(lingonXAliasPath(t), args...)
			source.Env = testAttachCLIEnv(home)
			if out, err := source.CombinedOutput(); err != nil {
				t.Fatalf("start lingonx: %v %s", err, out)
			}
			if transport == "relay" {
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
					if r.Role != "host" || r.Direction != ptytest.DirServerToClient {
						return false
					}
					var f protocolpb.Frame
					if proto.Unmarshal(r.Raw, &f) != nil {
						return false
					}
					for _, s := range f.GetSessions().GetSessions() {
						if s.Id == "osc8-cli" {
							return true
						}
					}
					return false
				})
				cancel()
				if err != nil {
					t.Fatal(err)
				}
			}
			start := func() *ptytest.PTYSession {
				if transport == "local" {
					return startLingonAttachHeadlessCLI(t, home, "osc8-cli", 80, 16)
				}
				return startLingonAttachCLI(t, h, "osc8-cli", 80, 16)
			}
			wait := func(v *ptytest.PTYSession, check func(ptytest.Screen) bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := v.WaitForScreen(ctx, check); err != nil {
					t.Fatalf("%v\n%s", err, v.Screen().String())
				}
			}
			viewer := start()
			t.Cleanup(viewer.Cancel)
			wait(viewer, func(s ptytest.Screen) bool { return s.Contains("OSC8_READY") })
			for _, step := range []struct{ command, url string }{{"paint", "https://example.invalid/one"}, {"other", "https://example.invalid/two"}} {
				viewer.DrainRaw()
				var raw string
				viewer.Send(step.command + "\r")
				wait(viewer, func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return s.Contains("CLI_DONE:" + step.command) })
				if !strings.Contains(raw, "\x1b]8;id=cli;"+step.url) {
					t.Fatal("CLI dropped OSC8 URI")
				}
			}
			viewer.Cancel()
			viewer = start()
			t.Cleanup(viewer.Cancel)
			var raw string
			wait(viewer, func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return s.Contains("CLI_DONE:other") })
			if !strings.Contains(raw, "https://example.invalid/two") {
				t.Fatal("reattach snapshot lost current target")
			}
			viewer.Send("history\r")
			wait(viewer, func(s ptytest.Screen) bool { return s.Contains("CLI_DONE:history") })
			viewer.DrainRaw()
			raw = ""
			viewer.Send("\x0c[g")
			wait(viewer, func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return s.Contains("SCROLL_LINK") })
			if !strings.Contains(raw, "\x1b]8;id=history;https://example.invalid/history") {
				t.Fatal("CLI scrollback lost hyperlink target")
			}
		})
	}
}
