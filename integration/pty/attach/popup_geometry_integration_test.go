//go:build integration

package integrationptyattach_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func popupSizeProbe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "popup-size-probe")
	script := `#!/bin/sh
initial=$(stty size)
printf '\nREADY> '
while IFS= read -r line; do
  set -- $initial
  printf '\nINITIAL=%sx%s ENV=%sx%s\n' "$2" "$1" "${COLUMNS-unset}" "${LINES-unset}"
  set -- $(stty size)
  printf 'LIVE=%sx%s REPLY:%s\n' "$2" "$1" "$line"
  if [ "$line" = DRAW ]; then
    printf '\033[HBOX_TOP\033[1;%sHR\033[%s;1HBOX_BOTTOM\033[%s;%sHB' "$2" "$1" "$1" "$2"
  else
    printf 'READY> '
  fi
done
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPopupGeometryAtApplicationStartupCLI(t *testing.T) {
	for _, mode := range []string{"local-attach", "local-restore", "lingon-x", "lingonx", "relay-attach"} {
		t.Run(mode, func(t *testing.T) {
			// Deliberately export the enclosing terminal's geometry. Programs
			// consulting these variables must not inherit it into a smaller PTY.
			t.Setenv("LINES", "60")
			t.Setenv("COLUMNS", "140")
			h := newHarness(t)
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			const parent = "popup-size-parent"
			args := []string{"-x", "--session", parent, "--cols", "80", "--rows", "20", "--shell", popupSizeProbe(t)}
			if mode == "relay-attach" {
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
			switch mode {
			case "relay-attach":
				waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
				viewer = startLingonAttachCLI(t, h, parent, 80, 20)
			case "lingon-x":
				viewer = startLingonRootHeadlessAttachCLI(t, home, parent, 80, 20)
			case "lingonx":
				viewer = startLingonXAttachHeadlessCLI(t, home, parent, 80, 20)
			case "local-restore":
				viewer = startLingonAttachHeadlessCLI(t, home, parent, 80, 20, "--restore")
			default:
				viewer = startLingonAttachHeadlessCLI(t, home, parent, 80, 20)
			}
			t.Cleanup(viewer.Cancel)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
			})
			viewer.Send("probe\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:probe") })
			pane := strings.Join(viewer.Screen().Lines[11:], "\n")
			if !strings.Contains(pane, "INITIAL=80x9") || !strings.Contains(pane, "LIVE=80x9") || strings.Contains(pane, "ENV=140x60") {
				t.Fatalf("application geometry was not bound to the popup box:\n%s", viewer.Screen().String())
			}
			viewer.Send("DRAW\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(11), "BOX_TOP") && strings.HasSuffix(s.Row(11), "R") && strings.Contains(s.Row(19), "BOX_BOTTOM") && strings.HasSuffix(s.Row(19), "B")
			})
			viewer.Send("\x0cjparent-probe\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-probe") })
			if !viewer.Screen().Contains("LIVE=80x20") {
				t.Fatal("popup changed the parent's normal terminal size")
			}
			viewer.Resize(90, 30)
			viewer.Send("parent-resized\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("LIVE=90x30 REPLY:parent-resized") })
			viewer.Send("\x0cjresized\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("LIVE=90x14 REPLY:resized") })
			viewer.Send("DRAW\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(16), "BOX_TOP") && strings.Contains(s.Row(29), "BOX_BOTTOM")
			})
			viewer.Resize(90, 80)
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(56), "j hide") })
			viewer.Send("capped\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("LIVE=90x23 REPLY:capped") })
			viewer.Send("DRAW\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(57), "BOX_TOP") && strings.HasSuffix(s.Row(57), "R") && strings.Contains(s.Row(79), "BOX_BOTTOM") && strings.HasSuffix(s.Row(79), "B")
			})
			viewer.Send("\x0cjparent-capped\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("LIVE=90x80 REPLY:parent-capped") })
		})
	}
}
