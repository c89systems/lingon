package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostSearchLocalSelectionSurvivesAnotherHostStarting(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	local := h.StartHost(ptytest.HostOptions{SessionID: "local", SessionName: "alpha", Cols: 100, Rows: 16, Shell: searchShell(t)})
	t.Cleanup(local.Cancel)
	remote := h.StartHost(ptytest.HostOptions{SessionID: "remote", SessionName: "beta", Cols: 100, Rows: 16, Shell: searchShell(t)})
	t.Cleanup(remote.Cancel)
	for _, spec := range []struct {
		terminal   *ptytest.PTYSession
		marker, id string
	}{{local, "local-origin", "local"}, {remote, "remote-origin", "remote"}} {
		waitSearchScreen(t, spec.terminal, func(s ptytest.Screen) bool { return s.Contains("READY>") })
		spec.terminal.Send(spec.marker + "\r")
		waitSearchScreen(t, spec.terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:" + spec.marker) })
		waitRestoreHost(t, recorder, spec.id)
	}
	waitSearchScreen(t, local, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "beta") })
	local.Send("\x0c/beta\r")
	waitSearchScreen(t, local, func(s ptytest.Screen) bool { return s.Contains("REPLY:remote-origin") })
	local.Send("\x0c/alpha\r")
	waitSearchScreen(t, local, func(s ptytest.Screen) bool { return s.Contains("REPLY:local-origin") })
	other := h.StartHost(ptytest.HostOptions{SessionID: "newcomer", SessionName: "gamma", Cols: 100, Rows: 16, Shell: searchShell(t)})
	t.Cleanup(other.Cancel)
	// The new tab is a session-list processing barrier in the original host.
	waitSearchScreen(t, local, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(0), "gamma") && s.Contains("REPLY:local-origin") && !s.Contains("REPLY:remote-origin")
	})
	local.Send("still-local\r")
	waitSearchScreen(t, local, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:still-local") && s.Contains("REPLY:local-origin")
	})
}
