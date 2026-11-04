package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostPopupsFollowParentTabsAndExplicitClosePTY(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "popup-tabs", Shell: searchShell(t), Cols: 100, Rows: 20})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "popup-tabs-2") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("one-popup\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:one-popup") })
	host.Send("\x0cc")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("READY>") && !s.Contains("j hide") && !s.Contains("REPLY:one-popup")
	})
	host.Send("second-parent\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:second-parent") })
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "popup-tabs-4") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("two-popup\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:two-popup") })
	host.Send("\x0ceTools\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "Tools") && !s.Contains("Rename session")
	})
	if strings.Contains(host.Screen().Row(0), "Tools") {
		t.Fatal("renaming a popup exposed it as a tab")
	}
	host.DrainRaw()
	host.Send("\x0cn")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:one-popup") && !s.Contains("REPLY:two-popup") })
	host.Send("\x0cp")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:two-popup") && s.Contains("REPLY:second-parent") })
	if strings.Contains(host.DrainRaw(), "\x1b[2J") {
		t.Fatal("tab switching with popups cleared the screen")
	}
	host.Send("\x0cQ")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:second-parent") && !s.Contains("j hide") && !s.Contains("REPLY:two-popup")
	})
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "popup-tabs-5") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("\x0cj\x0cJ\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(10), "popup-tabs-6") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	if strings.Contains(host.Screen().Row(0), "popup-tabs-2") || strings.Contains(host.Screen().Row(0), "popup-tabs-6") {
		t.Fatal("popup sessions leaked into navigation")
	}
}
