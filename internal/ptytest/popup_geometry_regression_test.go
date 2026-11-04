package ptytest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

// The fixture reads both geometry sources used by terminal programs. Unlike an
// interactive shell, it does not silently repair inherited LINES/COLUMNS.
func popupGeometryShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "popup-geometry")
	script := `#!/bin/sh
initial=$(stty size)
printf '\nREADY> '
while IFS= read -r line; do
  set -- $initial
  printf '\nINITIAL=%sx%s ' "$2" "$1"
  set -- $(stty size)
  printf 'PTY=%sx%s ENV=%sx%s\n' "$2" "$1" "${COLUMNS-unset}" "${LINES-unset}"
  printf 'REPLY:%s\nREADY> ' "$line"
  if [ "$line" = DRAW ]; then
    printf '\033[HBOX_TOP\033[1;%sHR\033[%s;1HBOX_BOTTOM\033[%s;%sHB' "$2" "$1" "$1" "$2"
  fi
done
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHostPopupPTYMatchesVisibleBoxWithPendingParentGeometry(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	parentSize := terminal.WindowSize{Cols: 120, Rows: 50}
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-geometry", Shell: popupGeometryShell(t), Cols: 80, Rows: 20, Geometry: &parentSize, DisablePublish: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("popup-size\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-size") })
	if !host.Screen().Contains("INITIAL=80x9 PTY=80x9") {
		t.Fatalf("popup PTY differs from its 80x9 content box:\n%s", host.Screen().String())
	}
	host.Send("\x0cjparent-size\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-size") })
	if !host.Screen().Contains("PTY=120x50") {
		t.Fatal("sizing the popup changed the parent's preserved geometry")
	}
}

func TestHostPopupGeometryStaysBoundOnInputAndWindowResize(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-resize", Shell: popupGeometryShell(t), Cols: 80, Rows: 20, DisablePublish: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	for _, marker := range []string{"first", "second"} {
		host.Send(marker + "\r")
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + marker) })
		pane := strings.Join(host.Screen().Lines[11:], "\n")
		lastSize := strings.LastIndex(pane, "PTY=")
		if lastSize < 0 || !strings.HasPrefix(pane[lastSize:], "PTY=80x9 ") || !strings.Contains(pane, "INITIAL=80x9") {
			t.Fatalf("input reset popup to parent size:\n%s", host.Screen().String())
		}
	}
	host.Send("DRAW\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(11), "BOX_TOP") && strings.HasSuffix(s.Row(11), "R") && strings.Contains(s.Row(19), "BOX_BOTTOM") && strings.HasSuffix(s.Row(19), "B")
	})
	host.Resize(90, 30)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), "j hide") })
	host.Send("resized\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:resized") })
	if !host.Screen().Contains("PTY=90x14") {
		t.Fatalf("popup did not follow its resized box:\n%s", host.Screen().String())
	}
	host.Send("\x0cjparent-resized\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-resized") })
	if !host.Screen().Contains("PTY=90x30") {
		t.Fatalf("parent did not retain normal window-resize behavior:\n%s", host.Screen().String())
	}
	host.Send("\x0cjDRAW\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(16), "BOX_TOP") && strings.Contains(s.Row(29), "BOX_BOTTOM")
	})
	host.Resize(90, 80)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(56), "j hide") })
	host.Send("capped\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:capped") })
	if !host.Screen().Contains("PTY=90x23") {
		t.Fatalf("popup PTY exceeded its capped content height:\n%s", host.Screen().String())
	}
	host.Send("DRAW\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(57), "BOX_TOP") && strings.HasSuffix(s.Row(57), "R") && strings.Contains(s.Row(79), "BOX_BOTTOM") && strings.HasSuffix(s.Row(79), "B")
	})
	host.Send("\x0cjparent-capped\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-capped") })
	if !host.Screen().Contains("PTY=90x80") {
		t.Fatal("popup's maximum height was incorrectly applied to the parent")
	}
}

func TestHostPopupDoesNotInheritParentEnvironmentGeometry(t *testing.T) {
	t.Setenv("LINES", "60")
	t.Setenv("COLUMNS", "140")
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-env", Shell: popupGeometryShell(t), Cols: 80, Rows: 20, DisablePublish: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("popup-env\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-env") })
	if host.Screen().Contains("ENV=140x60") {
		t.Fatalf("popup inherited enclosing terminal geometry:\n%s", host.Screen().String())
	}
}
