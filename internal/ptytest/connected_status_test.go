package ptytest_test

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestConnectedStatusOnlySaysConnectedHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "multi-attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "connection-label", Shell: searchShell(t), Cols: 100, Rows: 20})
			t.Cleanup(host.Cancel)
			waitRestoreHost(t, recorder, "connection-label")
			viewer := host
			switch mode {
			case "multi-attach":
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "connection-label", Cols: 100, Rows: 20})
			}
			t.Cleanup(viewer.Cancel)
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") && strings.Contains(s.Row(0), "connected") })
			row := strings.TrimSpace(viewer.Screen().Row(0))
			if !strings.HasSuffix(row, "connected") || strings.Contains(row, "connected to") || strings.Contains(row, "http") {
				t.Fatalf("expected short connected status without endpoint: %q", row)
			}
			viewer.Send("status-check\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:status-check") })
		})
	}
}
