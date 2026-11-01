package ptytest_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func fmtGeometry(size terminal.WindowSize) string { return fmt.Sprintf("%dx%d", size.Cols, size.Rows) }
func fmtReply(marker string, cols, rows int) string {
	return fmt.Sprintf("REPLY:%s SIZE:%d %d", marker, rows, cols)
}

func TestHostGeometryRequestsOuterWindowAndPreservesPTYUntilAcknowledged(t *testing.T) {
	for _, size := range []terminal.WindowSize{{Cols: 100, Rows: 50}, {Cols: 100}, {Rows: 50}} {
		t.Run(fmtGeometry(size), func(t *testing.T) {
			clk := clock.NewMock()
			h := ptytest.New(t, ptytest.WithClock(clk))
			host := h.StartHost(ptytest.HostOptions{SessionID: "geometry", Cols: 80, Rows: 24, Geometry: &size, Shell: restoreShell(t), DisablePublish: true})
			t.Cleanup(host.Cancel)
			cols, rows := size.Cols, size.Rows
			if cols == 0 {
				cols = 80
			}
			if rows == 0 {
				rows = 24
			}
			waitRestoreRequest(t, host, cols, rows)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			clk.Add(time.Second)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("Terminal did not accept requested geometry") })
			host.Send("before-ack\r")
			want := fmtReply("before-ack", cols, rows)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(want) })
			host.Resize(cols, rows)
			host.Send("after-ack\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("after-ack", cols, rows)) })
			host.Resize(90, 30)
			host.Send("manual-resize\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual-resize", 90, 30)) })
		})
	}
}

func TestAttachGeometryResizesBothTerminalsAndRemotePTY(t *testing.T) {
	for _, multi := range []bool{false, true} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("multi=%v/headless=%v", multi, allow), func(t *testing.T) {
				recorder := ptytest.NewWSRecorder()
				h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
				host := h.StartHost(ptytest.HostOptions{SessionID: "geometry", SessionName: "geometry", Cols: 90, Rows: 30, Shell: restoreShell(t), AllowRemoteResize: allow})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				waitRestoreHost(t, recorder, "geometry")
				size := terminal.WindowSize{Cols: 100, Rows: 50}
				var client *ptytest.PTYSession
				if multi {
					client = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "geometry", Cols: 80, Rows: 24, Geometry: &size})
				} else {
					client = h.StartAttach(ptytest.AttachOptions{SessionID: "geometry", Cols: 80, Rows: 24, Geometry: &size, RequestControl: true})
				}
				t.Cleanup(client.Cancel)
				waitRestoreRequest(t, client, 100, 50)
				waitRestoreRequest(t, host, 100, 50)
				client.Send("before-ack\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("before-ack", 100, 50)) })
				client.Resize(100, 50)
				host.Resize(100, 50)
				client.Send("after-ack\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("after-ack", 100, 50)) })
				client.Resize(95, 35)
				waitGeometrySnapshot(t, recorder, "geometry", 95, 35)
				client.Send("manual\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual", 95, 35)) })
			})
		}
	}
}

func waitGeometrySnapshot(t *testing.T, recorder *ptytest.WSRecorder, id string, cols, rows int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
		if r.Role != "host" || r.Direction != ptytest.DirClientToServer || r.SessionID != id {
			return false
		}
		var f protocolpb.Frame
		return proto.Unmarshal(r.Raw, &f) == nil && int(f.GetSnapshot().GetCols()) == cols && int(f.GetSnapshot().GetRows()) == rows
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAttachGeometryPartialDimensionsAndRestorePrecedence(t *testing.T) {
	for _, multi := range []bool{false, true} {
		for _, tc := range []struct {
			size       terminal.WindowSize
			restore    bool
			cols, rows int
		}{
			{terminal.WindowSize{Cols: 100}, false, 100, 24},
			{terminal.WindowSize{Rows: 50}, false, 80, 50},
			{terminal.WindowSize{Cols: 100, Rows: 50}, true, 90, 30},
		} {
			t.Run(fmt.Sprintf("multi=%v/%s/restore=%v", multi, fmtGeometry(tc.size), tc.restore), func(t *testing.T) {
				recorder := ptytest.NewWSRecorder()
				h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
				host := h.StartHost(ptytest.HostOptions{SessionID: "partial", Cols: 90, Rows: 30, Shell: restoreShell(t)})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				waitRestoreHost(t, recorder, "partial")
				var client *ptytest.PTYSession
				if multi {
					client = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "partial", Cols: 80, Rows: 24, Geometry: &tc.size, Restore: tc.restore})
				} else {
					client = h.StartAttach(ptytest.AttachOptions{SessionID: "partial", Cols: 80, Rows: 24, Geometry: &tc.size, Restore: tc.restore, RequestControl: true})
				}
				t.Cleanup(client.Cancel)
				waitRestoreRequest(t, client, tc.cols, tc.rows)
				client.Send("partial\r")
				waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("partial", tc.cols, tc.rows)) })
				if tc.restore {
					assertNoRestorePropagation(t, recorder)
				}
			})
		}
	}
}

func TestAttachGeometryFollowsSelectedTab(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	for _, id := range []string{"alpha", "beta"} {
		host := h.StartHost(ptytest.HostOptions{SessionID: id, SessionName: id, Cols: 90, Rows: 30, Shell: restoreShell(t)})
		t.Cleanup(host.Cancel)
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
		waitRestoreHost(t, recorder, id)
	}
	size := terminal.WindowSize{Cols: 100, Rows: 50}
	client := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Geometry: &size})
	t.Cleanup(client.Cancel)
	waitRestoreRequest(t, client, 100, 50)
	client.Resize(100, 50)
	client.Send("alpha\r")
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("alpha", 100, 50)) })
	client.Send("\x0cn")
	client.Send("beta\r")
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("beta", 100, 50)) })
	client.Resize(95, 35)
	waitGeometrySnapshot(t, recorder, "beta", 95, 35)
	client.Send("manual-beta\r")
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual-beta", 95, 35)) })
	// Activate an already connected view: it must receive the current window size.
	client.Send("\x0cp")
	waitGeometrySnapshot(t, recorder, "alpha", 95, 35)
	client.Send("manual-alpha\r")
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("manual-alpha", 95, 35)) })
}

func TestRemoteGeometrySurvivesInactiveHostTabActivation(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	var watching, reverted atomic.Bool
	host := h.StartHost(ptytest.HostOptions{SessionID: "inactive", SessionName: "alpha", Cols: 90, Rows: 30, Shell: restoreShell(t), OnPublishFrame: func(f *protocolpb.Frame) {
		if watching.Load() && f.SessionId == "inactive" && f.GetSnapshot() != nil && (f.GetSnapshot().Cols != 100 || f.GetSnapshot().Rows != 50) {
			reverted.Store(true)
		}
	}})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("\x0cc")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("alpha-2") && s.Contains("READY>") })
	host.Send("second\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("second", 90, 30)) })
	waitRestoreHost(t, recorder, "inactive")
	host.DrainRaw()
	size := terminal.WindowSize{Cols: 100, Rows: 50}
	client := h.StartAttach(ptytest.AttachOptions{SessionID: "inactive", Cols: 80, Rows: 24, Geometry: &size, RequestControl: true})
	t.Cleanup(client.Cancel)
	waitRestoreRequest(t, client, 100, 50)
	client.Send("remote\r")
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains(fmtReply("remote", 100, 50)) })
	host.Send("unchanged\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("unchanged", 90, 30)) })
	if strings.Contains(host.DrainRaw(), "\x1b[8;50;100t") {
		t.Fatal("inactive remote target resized the active host window")
	}
	watching.Store(true)
	host.Send("\x0cp")
	waitRestoreRequest(t, host, 100, 50)
	host.Send("activated\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("activated", 100, 50)) })
	if reverted.Load() {
		t.Fatal("activating the host tab transiently reverted explicit remote geometry")
	}
}
