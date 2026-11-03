package ptytest_test

import (
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRestoreManualResizeUpdatesOnlySelectedSessionPTY(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%v", multi), func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			for _, spec := range []struct {
				id         string
				cols, rows int
			}{{"alpha", 100, 30}, {"beta", 95, 28}} {
				host := h.StartHost(ptytest.HostOptions{SessionID: spec.id, SessionName: spec.id, Cols: spec.cols, Rows: spec.rows, Shell: restoreShell(t)})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				waitRestoreHost(t, recorder, spec.id)
			}
			var client *ptytest.PTYSession
			if multi {
				client = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Restore: true})
			} else {
				client = h.StartAttach(ptytest.AttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Restore: true, RequestControl: true})
			}
			t.Cleanup(client.Cancel)
			waitRestoreRequest(t, client, 100, 30)
			client.Resize(100, 30) // Acknowledge the initial programmatic resize.
			client.Send("initial\r")
			waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("initial", 100, 30)) })
			client.DrainRaw()
			client.Resize(88, 26) // A manual resize must be accepted, not undone.
			waitGeometrySnapshot(t, recorder, "alpha", 88, 26)
			client.Send("manual\r")
			waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual", 88, 26)) })
			if raw := client.DrainRaw(); strings.Contains(raw, "\x1b[8;30;100t") {
				t.Fatal("restore fought the manual window resize")
			}
			client.Send("\x0c0redraw\r")
			waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("redraw", 88, 26)) })
			if multi {
				client.Send("\x0cn")
				waitRestoreRequest(t, client, 95, 28)
				client.Resize(95, 28)
				client.Send("other\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("other", 95, 28)) })
				client.Send("\x0cp")
				waitRestoreRequest(t, client, 88, 26)
				client.Resize(88, 26)
				client.Send("returned\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("returned", 88, 26)) })
			}
		})
	}
}
