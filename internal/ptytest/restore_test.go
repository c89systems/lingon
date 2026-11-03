package ptytest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func restoreShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '\\nREADY> '\nwhile IFS= read -r line; do printf '\\nREPLY:%s SIZE:%s\\nREADY> ' \"$line\" \"$(stty size)\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitRestoreRequest(t *testing.T, terminal *ptytest.PTYSession, cols, rows int) {
	t.Helper()
	want := fmt.Sprintf("\x1b[8;%d;%dt", rows, cols)
	var raw string
	waitSearchScreen(t, terminal, func(_ ptytest.Screen) bool {
		raw += terminal.DrainRaw()
		return strings.Contains(raw, want)
	})
}

func assertNoRestorePropagation(t *testing.T, recorder *ptytest.WSRecorder) {
	t.Helper()
	for _, record := range recorder.Frames() {
		if record.Role != "client" || record.Direction != ptytest.DirClientToServer {
			continue
		}
		var frame protocolpb.Frame
		if err := proto.Unmarshal(record.Raw, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.GetResize() != nil {
			t.Fatal("restore mode sent a resize frame")
		}
		if hello := frame.GetHello(); hello != nil && (hello.Cols != 0 || hello.Rows != 0) {
			t.Fatalf("restore hello supplied local size: %v", hello)
		}
	}
}

func waitRestoreHost(t *testing.T, recorder *ptytest.WSRecorder, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
		if r.Role != "host" || r.Direction != ptytest.DirServerToClient {
			return false
		}
		var frame protocolpb.Frame
		if proto.Unmarshal(r.Raw, &frame) != nil {
			return false
		}
		for _, session := range frame.GetSessions().GetSessions() {
			if session.Id == id {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreAcknowledgesWindowRequestAndAcceptsManualResizePTY(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%v", multi), func(t *testing.T) {
			clk := clock.NewMock()
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "restore", SessionName: "restore", Shell: restoreShell(t), Cols: 100, Rows: 30, AllowRemoteResize: true})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			waitRestoreHost(t, recorder, "restore")
			var terminal *ptytest.PTYSession
			if multi {
				terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "restore", Cols: 80, Rows: 24, Restore: true})
			} else {
				terminal = h.StartAttach(ptytest.AttachOptions{SessionID: "restore", Cols: 80, Rows: 24, Restore: true, RequestControl: true})
			}
			t.Cleanup(terminal.Cancel)
			waitRestoreRequest(t, terminal, 100, 30)
			// Model an emulator accepting the request, using only its owned sub-PTY.
			terminal.Resize(100, 30)
			terminal.Send("preserved\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:preserved SIZE:30 100") })
			assertNoRestorePropagation(t, recorder)
			// A manual window resize updates this session instead of restoring over it.
			terminal.DrainRaw()
			terminal.Resize(90, 25)
			waitGeometrySnapshot(t, recorder, "restore", 90, 25)
			terminal.Send("still-preserved\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:still-preserved SIZE:25 90") })
			// Toggle off restores the established local-to-remote direction.
			terminal.Send("\x0c!")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("Restore off") })
			terminal.Resize(88, 26)
			// A resize reply on the snapshot stream fences the PTY size change.
			if err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
				if r.Role != "host" || r.Direction != ptytest.DirClientToServer {
					return false
				}
				var frame protocolpb.Frame
				if proto.Unmarshal(r.Raw, &frame) != nil {
					return false
				}
				return frame.GetSnapshot().GetCols() == 88 && frame.GetSnapshot().GetRows() == 26
			}); err != nil {
				t.Fatal(err)
			}
			terminal.Send("normal\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:normal SIZE:26 88") })
			terminal.Send("\x0c!")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("follow session size") })
			terminal.DrainRaw()
			terminal.Resize(75, 20)
			waitGeometrySnapshot(t, recorder, "restore", 75, 20)
			terminal.Send("restored-again\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:restored-again SIZE:20 75") })
		})
	}
}

func TestRestoreFollowsSelectedSessionPTY(t *testing.T) {
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	for _, spec := range []struct {
		id         string
		cols, rows int
	}{{"alpha", 100, 30}, {"beta", 95, 28}} {
		host := h.StartHost(ptytest.HostOptions{SessionID: spec.id, SessionName: spec.id, Shell: restoreShell(t), Cols: spec.cols, Rows: spec.rows, AllowRemoteResize: true})
		t.Cleanup(host.Cancel)
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
		waitRestoreHost(t, recorder, spec.id)
	}
	terminal := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "alpha", Cols: 80, Rows: 24, Restore: true})
	t.Cleanup(terminal.Cancel)
	waitRestoreRequest(t, terminal, 100, 30)
	terminal.Resize(100, 30)
	terminal.Send("\x0cn")
	waitRestoreRequest(t, terminal, 95, 28)
	terminal.Resize(95, 28)
	terminal.Send("beta-input\r")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:beta-input SIZE:28 95") })
	terminal.DrainRaw()
	terminal.Send("\x0c/alp\r")
	waitRestoreRequest(t, terminal, 100, 30)
	terminal.Send("alpha-input\r")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:alpha-input SIZE:30 100") && !s.Contains("REPLY:beta-input")
	})
	assertNoRestorePropagation(t, recorder)
}
