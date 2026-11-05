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
	"pkt.systems/lingon/internal/terminal"
)

func TestImportantTabsLocalAndRelayCLI(t *testing.T) {
	for _, transport := range []string{"local", "relay"} {
		t.Run(transport, func(t *testing.T) {
			h := newHarness(t)
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			for _, id := range []string{"important-a", "important-b"} {
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
			}
			start := func(id string) *ptytest.PTYSession {
				if transport == "relay" {
					return startLingonAttachCLI(t, h, id, 100, 20)
				}
				return startLingonAttachHeadlessCLI(t, home, id, 100, 20)
			}
			viewer := start("important-a")
			t.Cleanup(viewer.Cancel)
			wait := func(view *ptytest.PTYSession, check func(ptytest.Screen) bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := view.WaitForScreen(ctx, check); err != nil {
					t.Fatalf("screen: %v\n%s", err, view.Screen().String())
				}
			}
			field := func(want string) {
				t.Helper()
				wait(viewer, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(19)) == want })
			}
			color := func(view *ptytest.PTYSession, name string) terminal.Cell {
				x := strings.Index(view.Screen().Row(0), name)
				if x < 0 {
					return terminal.Cell{}
				}
				return view.Snapshot().Cells[x]
			}
			wait(viewer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
			viewer.Send("printf 'FIRST_%s\\n' READY\r\x0c/")
			wait(viewer, func(s ptytest.Screen) bool {
				return s.Contains("FIRST_READY") && strings.TrimSpace(s.Row(19)) == "/" && strings.Contains(s.Row(0), "important-b")
			})
			active := color(viewer, "important-a")
			inactive := color(viewer, "important-b")
			fill := viewer.Snapshot().Cells[99].BG
			viewer.Send("\x1b\x0ci\x0c/")
			wait(viewer, func(s ptytest.Screen) bool {
				return color(viewer, "important-a").BG == active.BG && color(viewer, "important-b").BG != inactive.BG && viewer.Snapshot().Cells[99].BG != fill
			})
			viewer.Send("\x1b\x0c?important-b\r")
			field("/ important-b")
			if strings.Contains(viewer.Screen().Row(0), "[") {
				t.Fatal("important-only search selected an unmarked tab")
			}
			viewer.Send("\x1b\x0c/important-b\rprintf 'SECOND_%s\\n' READY\r")
			wait(viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_READY") && !s.Contains("FIRST_READY") })
			viewer.Send("\x0cj")
			wait(viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			viewer.Send("printf 'POPUP_%s\\n' READY\r\x0c\x09\x0c/")
			wait(viewer, func(s ptytest.Screen) bool {
				return s.Contains("POPUP_READY") && strings.TrimSpace(s.Row(19)) == "/" && color(viewer, "important-b").BG == active.BG
			})
			if viewer.Snapshot().Cells[99].BG == fill {
				t.Fatal("marking every tab left important mode")
			}
			viewer.Send("\x1b\x0cj\x0c?\t")
			field("/ important-a")
			viewer.Send("\x1b[Z")
			field("/ important-b")
			// A second actual attach process has an independent importance set.
			other := start("important-b")
			t.Cleanup(other.Cancel)
			wait(other, func(s ptytest.Screen) bool { return s.Contains("SECOND_READY") })
			other.Send("\x0c/")
			wait(other, func(s ptytest.Screen) bool {
				return strings.TrimSpace(s.Row(19)) == "/" && other.Snapshot().Cells[99].BG == fill
			})
			viewer.Send("\x1b\x0cI\x0c?\t")
			field("/ important-a")
			if color(viewer, "important-a").BG != inactive.BG || viewer.Snapshot().Cells[99].BG != fill {
				t.Fatal("reset did not restore the original tab theme")
			}
			viewer.Send("\x1b[Z")
			field("/ important-b")
		})
	}
}
