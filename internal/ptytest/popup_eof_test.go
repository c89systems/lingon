package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostPopupCtrlDLogsOutWithoutClosingParentPTY(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-eof", Shell: searchShell(t), Cols: 100, Rows: 20, DisablePublish: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("parent\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("popup\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup") })
	host.Send("\x04")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:parent") && !s.Contains("j hide") && !s.Contains("REPLY:popup")
	})
	host.Send("parent-survived\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-survived") })
}
