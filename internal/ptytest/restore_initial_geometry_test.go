package ptytest_test

import (
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestRestoreInitialGeometryThenPerSessionSizingPTY(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%v", multi), func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			for _, spec := range []struct {
				id         string
				cols, rows int
			}{{"alpha", 90, 30}, {"beta", 95, 28}} {
				host := h.StartHost(ptytest.HostOptions{SessionID: spec.id, SessionName: spec.id, Cols: spec.cols, Rows: spec.rows, Shell: restoreShell(t)})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				waitRestoreHost(t, recorder, spec.id)
			}
			size := terminal.WindowSize{Cols: 100, Rows: 50}
			var client *ptytest.PTYSession
			if multi {
				client = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Geometry: &size, Restore: true})
			} else {
				client = h.StartAttach(ptytest.AttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Geometry: &size, Restore: true, RequestControl: true})
			}
			t.Cleanup(client.Cancel)
			// The target remote grid must be published before the window follows it.
			waitGeometrySnapshot(t, recorder, "alpha", 100, 50)
			var raw string
			waitSearchScreen(t, client, func(_ ptytest.Screen) bool { raw += client.DrainRaw(); return strings.Contains(raw, "\x1b[8;50;100t") })
			if strings.Contains(raw, "\x1b[8;30;90t") {
				t.Fatal("restored old remote size before applying initial geometry")
			}
			client.Resize(100, 50)
			client.Send("seeded\r")
			waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("seeded", 100, 50)) })
			client.Resize(88, 26)
			waitGeometrySnapshot(t, recorder, "alpha", 88, 26)
			client.Send("manual\r")
			waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual", 88, 26)) })
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
