//go:build integration

package integrationptyattach_test

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

// Keep the UI and host in the test process so -race covers their live callbacks;
// only ordinary managed-child creation reexecutes the isolated CLI executable.
func TestPopupConcurrentHostAndAttachFramesPTY(t *testing.T) {
	h := newHarness(t, ptytest.WithClock(clock.NewMock()))
	cfg := shortHomeDir(t)
	cleanupManagedTerminals(t, cfg)
	const parent = "concurrent-popup-parent"
	host := h.StartHost(ptytest.HostOptions{SessionID: parent, Shell: fixedPromptEmitRowsBash(t), Cols: 100, Rows: 20,
		ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t), AllowRemoteResize: true})
	t.Cleanup(host.Cancel)
	popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: parent, Cols: 100, Rows: 20})
	t.Cleanup(viewer.Cancel)
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
	viewer.Send("\x0cj")
	popupScreen(t, viewer, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
	})
	viewer.Send("printf 'POPUP_%s\\n' CONCURRENT\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("POPUP_CONCURRENT") })
	host.Send("printf 'BASE_%s\\n' CONCURRENT\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("BASE_CONCURRENT") && s.Contains("POPUP_CONCURRENT") })
	viewer.Resize(90, 30)
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), "j hide") })
	viewer.Send("stty size; printf 'RESIZE_%s\\n' DONE\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("14 90") && s.Contains("RESIZE_DONE") })
	viewer.Send("\x0ceLive tools\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(15), "Live_tools") && !s.Contains("Rename session")
	})
	viewer.Send("\x0cj\x0cJ")
	viewer.Send("printf 'PARENT_%s\\n' REMAINS\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_REMAINS") && !s.Contains("j hide") })
}
