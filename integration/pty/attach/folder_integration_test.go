//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func TestFolderLocalAndRelayCreationCLI(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		for _, popup := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/popup=%v", transport, popup), func(t *testing.T) {
				h := newHarness(t)
				home := shortHomeDir(t)
				cfg := filepath.Join(home, lingon.DefaultConfigDirName)
				cleanupManagedTerminals(t, cfg)
				base := t.TempDir()
				destination := filepath.Join(base, "new folder")
				if err := os.Mkdir(destination, 0700); err != nil {
					t.Fatal(err)
				}
				shell := fixedPromptEmitRowsBash(t)
				if transport == "relay" {
					caller, err := os.Getwd()
					if err != nil {
						t.Fatal(err)
					}
					relative, err := filepath.Rel(caller, shell)
					if err != nil {
						t.Fatal(err)
					}
					shell = relative
				} else {
					script, err := os.ReadFile(shell)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(base, "shell"), script, 0700); err != nil {
						t.Fatal(err)
					}
					shell = "./shell"
				}
				source := "folder-source"
				if transport == "relay" {
					host := h.StartHost(ptytest.HostOptions{SessionID: source, WorkingDirectory: base, Shell: shell, Cols: 220, Rows: 24, ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
					t.Cleanup(host.Cancel)
					waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{source}, 8*time.Second)
				} else {
					cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--session", source, "--shell", shell)
					cmd.Dir = base
					cmd.Env = testAttachCLIEnv(home)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("source: %v %s", err, out)
					}
				}
				start := func() *ptytest.PTYSession {
					if transport == "relay" {
						return startLingonAttachCLI(t, h, source, 220, 24)
					}
					return startLingonAttachHeadlessCLI(t, home, source, 220, 24)
				}
				viewer := start()
				t.Cleanup(viewer.Cancel)
				wait := func(v *ptytest.PTYSession, check func(ptytest.Screen) bool) {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
					defer cancel()
					if err := v.WaitForScreen(ctx, check); err != nil {
						t.Fatalf("%v\n%s", err, v.Screen().String())
					}
				}
				field := func(v *ptytest.PTYSession, path string) {
					t.Helper()
					wait(v, func(s ptytest.Screen) bool {
						return strings.HasPrefix(strings.TrimSpace(s.Row(23)), "working directory: "+path) && !strings.Contains(s.Row(23), "loading")
					})
				}
				wait(viewer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
				viewer.Send("\x0cf")
				field(viewer, base+"/")
				viewer.Send("new\t")
				field(viewer, destination+"/")
				// Immediate following input is ordered after the save acknowledgement.
				viewer.Send("\rprintf 'PARENT:%s\\n' \"$PWD\"\r")
				wait(viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT:" + base) })
				// A fresh attach sees the same runtime setting without restarting the host.
				observer := start()
				t.Cleanup(observer.Cancel)
				wait(observer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
				observer.Send("\x0cf")
				field(observer, destination+"/")
				observer.Cancel()
				// Save a different destination and create immediately in the same
				// input batch, proving creation cannot overtake its acknowledgement.
				destination2 := filepath.Join(base, "next folder")
				if err := os.Mkdir(destination2, 0700); err != nil {
					t.Fatal(err)
				}
				viewer.Send("\x0cf")
				field(viewer, destination+"/")
				viewer.Send("\x15" + destination2)
				command := "c"
				if popup {
					command = "j"
				}
				viewer.Send("\r\x0c" + command)
				wait(viewer, func(s ptytest.Screen) bool {
					return s.Contains("PROMPT>") && !s.Contains("PARENT:"+base) || (popup && strings.Contains(strings.Join(s.Lines[13:], "\n"), "PROMPT>"))
				})
				viewer.Send("printf 'CHILD:%s\\n' \"$PWD\"\r")
				wait(viewer, func(s ptytest.Screen) bool { return s.Contains("CHILD:" + destination2) })
				// Local or remote shell creation never changes the caller process directory.
				caller, err := os.Getwd()
				if err != nil || caller == destination {
					t.Fatalf("caller cwd changed: %q %v", caller, err)
				}
			})
		}
	}
}
