package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostPopupHidesReusesAndPromotesPTY(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-parent", Shell: searchShell(t), Cols: 100, Rows: 20})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("parent-marker\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:parent-marker") })
	host.DrainRaw()
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "popup-parent-2") && strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	if !strings.Contains(host.Screen().Row(10), "J terminate") {
		t.Fatalf("popup termination hint missing: %q", host.Screen().Row(10))
	}
	if strings.Contains(host.Screen().Row(0), "popup-parent-2") {
		t.Fatal("popup leaked into tab bar")
	}
	host.Send("popup-marker\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:popup-marker") && s.Contains("REPLY:parent-marker")
	})
	host.Send("\x0cjparent-again\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:parent-again") && !s.Contains("REPLY:popup-marker")
	})
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-marker") && s.Contains("j hide") })
	if strings.Contains(host.DrainRaw(), "\x1b[2J") {
		t.Fatal("popup show/hide cleared the screen")
	}
	// Hide it and close the parent. Its child must become a regular tab.
	host.Send("\x0cj\x0cQ")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:popup-marker") && strings.Contains(s.Row(0), "popup-parent-2") && !s.Contains("j hide")
	})
	host.Send("promoted\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:promoted") })
}
