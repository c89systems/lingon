package ptytest_test

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachCloseTerminatesRemoteSessionPTY(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "close-target", Shell: searchShell(t), Cols: 100, Rows: 20})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cc")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("close-target-2") && s.Contains("READY>") })
	host.Send("survivor\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:survivor") })
	waitRestoreHost(t, recorder, "close-target")
	waitRestoreHost(t, recorder, "close-target-2")
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "close-target", Cols: 100, Rows: 20})
	t.Cleanup(viewer.Cancel)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	viewer.Send("\x0cQ")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:survivor") })
	viewer.Send("still-attached\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:still-attached") })
}
