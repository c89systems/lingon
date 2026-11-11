//go:build integration

package integrationptyattach_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachPopupRestoreToggleUsesParentWindowPTY(t *testing.T) {
	h := newHarness(t)
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	source := h.StartHost(ptytest.HostOptions{SessionID: "source", Cols: 100, Rows: 30, Shell: fixedPromptEmitRowsBash(t), ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
	t.Cleanup(source.Cancel)
	waitForPromptVisible(t, source, 8*time.Second)
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"source"}, 8*time.Second)
	viewer := startGeometryCLI(t, home, "attach", "source", "--restore", "--endpoint", h.Endpoint(), "--access-token", h.AccessToken(), "--insecure", "--request-control", "--disable-desktop-notifications")
	t.Cleanup(viewer.Cancel)
	wait := func(check func(ptytest.Screen) bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		if err := viewer.WaitForScreen(ctx, check); err != nil {
			t.Fatalf("%v\n%s", err, viewer.Screen().String())
		}
	}
	var raw string
	wait(func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return strings.Contains(raw, "\x1b[8;30;100t") })
	viewer.Resize(100, 30)
	wait(func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
	viewer.Send("\x0cj")
	wait(func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(15), "j hide") && strings.Contains(strings.Join(s.Lines[16:], "\n"), "PROMPT>")
	})
	viewer.Send("\x0c!")
	wait(func(s ptytest.Screen) bool { return s.Contains("Restore off") })
	viewer.Resize(80, 24)
	wait(func(s ptytest.Screen) bool { return strings.Contains(s.Row(12), "j hide") })
	viewer.DrainRaw()
	viewer.Send("\x0c!")
	wait(func(s ptytest.Screen) bool { return s.Contains("follow session size") })
	raw = ""
	wait(func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return strings.Contains(raw, "\x1b[8;30;100t") })
	viewer.Resize(100, 30)
	wait(func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), "j hide") })
	viewer.Send("printf 'POPUP_ACTIVE=%s\\n' \"$(stty size)\"\r")
	wait(func(s ptytest.Screen) bool {
		return strings.Contains(strings.Join(s.Lines[16:], "\n"), "POPUP_ACTIVE=14 100")
	})
	// The window-level action must leave input in the popup and preserve the
	// interactive parent's terminal dimensions.
	source.Send("printf 'PARENT_ACTIVE=%s\\n' \"$(stty size)\"\r")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := source.WaitForScreen(ctx, func(s ptytest.Screen) bool { return s.Contains("PARENT_ACTIVE=30 100") }); err != nil {
		t.Fatalf("parent geometry changed: %v\n%s", err, source.Screen().String())
	}
}
