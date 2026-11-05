package ptytest_test

import (
	"regexp"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

// All emulator requests and acknowledgements use test-owned PTYs. Screen/reply
// barriers fence each operation; mock time is never advanced implicitly.
func TestHostRestoreLocalRemoteRoundTripPTY(t *testing.T) {
	for _, scenario := range []string{"manual-remote", "manual-local", "delayed-ack", "toggle", "interactive-remote", "initial-geometry", "toggle-local", "toggle-pending-local"} {
		t.Run(scenario, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			remote := h.StartHost(ptytest.HostOptions{SessionID: "alpha", SessionName: "alpha", Cols: 100, Rows: 30, Shell: restoreShell(t), AllowRemoteResize: scenario != "interactive-remote"})
			t.Cleanup(remote.Cancel)
			waitSearchScreen(t, remote, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			waitRestoreHost(t, recorder, "alpha")
			var geometry *terminal.WindowSize
			if scenario == "initial-geometry" {
				geometry = &terminal.WindowSize{Cols: 84, Rows: 27}
			}
			host := h.StartHost(ptytest.HostOptions{SessionID: "home", SessionName: "home", Cols: 80, Rows: 24, Shell: restoreShell(t), Restore: scenario != "toggle-local", Geometry: geometry})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") && s.Contains("alpha") })
			reply := func(marker string, cols, rows int) {
				t.Helper()
				host.Send(marker + "\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply(marker, cols, rows)) })
			}
			localCols, localRows := 80, 24
			if scenario == "toggle-local" {
				host.Send("\x0c!")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("follow session size") })
			}
			if geometry != nil {
				// Startup geometry takes effect before restoration and survives a round trip.
				host.Resize(geometry.Cols, geometry.Rows)
				reply("initial-geometry", geometry.Cols, geometry.Rows)
				localCols, localRows = geometry.Cols, geometry.Rows
			}
			if scenario == "manual-local" {
				host.Resize(84, 27)
				reply("local-resized", 84, 27)
				localCols, localRows = 84, 27
			}
			host.Send("\x0c/alpha\r")
			waitRestoreRequest(t, host, 100, 30)
			reply("remote-preserved", 100, 30)
			if scenario != "delayed-ack" {
				host.Resize(100, 30)
				reply("remote-ack", 100, 30)
			}
			remoteCols, remoteRows := 100, 30
			if scenario == "manual-remote" || scenario == "interactive-remote" {
				host.Resize(88, 26)
				waitGeometrySnapshot(t, recorder, "alpha", 88, 26)
				reply("remote-manual", 88, 26)
				remoteCols, remoteRows = 88, 26
			}
			host.Send("\x0c/home\r")
			waitRestoreRequest(t, host, localCols, localRows)
			if scenario == "delayed-ack" {
				reply("before-late-ack", localCols, localRows)
				host.Resize(100, 30)
				reply("late-remote-ack", localCols, localRows)
			}
			host.Resize(localCols, localRows)
			reply("local-return", localCols, localRows)
			host.Send("\x0c/alpha\r")
			waitRestoreRequest(t, host, remoteCols, remoteRows)
			host.Resize(remoteCols, remoteRows)
			reply("remote-return", remoteCols, remoteRows)
			if scenario == "toggle-pending-local" {
				host.Send("\x0c/home\r")
				waitRestoreRequest(t, host, localCols, localRows)
				host.Send("\x0c!")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("Restore off") })
				reply("off-before-ack", remoteCols, remoteRows)
			}
			if scenario == "toggle" {
				host.Send("\x0c!")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("Restore off") })
				host.Resize(86, 25)
				waitGeometrySnapshot(t, recorder, "alpha", 86, 25)
				reply("normal-policy", 86, 25)
				host.Send("\x0c!")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("follow session size") })
			}
		})
	}
}

func TestHostDefaultRemoteResizePolicyPTY(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	remote := h.StartHost(ptytest.HostOptions{SessionID: "alpha", SessionName: "alpha", Cols: 100, Rows: 30, Shell: restoreShell(t), AllowRemoteResize: true})
	t.Cleanup(remote.Cancel)
	waitSearchScreen(t, remote, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "alpha")
	host := h.StartHost(ptytest.HostOptions{SessionID: "home", SessionName: "home", Cols: 80, Rows: 24, Shell: restoreShell(t)})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") && s.Contains("alpha") })
	host.DrainRaw()
	host.Send("\x0c/alpha\r")
	waitGeometrySnapshot(t, recorder, "alpha", 80, 24)
	host.Send("normal-policy\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("normal-policy", 80, 24)) })
	if raw := host.DrainRaw(); regexp.MustCompile("\x1b\\[8;[0-9]+;[0-9]+t").MatchString(raw) {
		t.Fatalf("default host forced a window resize: %q", raw)
	}
	host.Send("\x0c/home\rlocal-normal\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply("local-normal", 80, 24)) })
}

func TestHostRestoreLocalTabsAndPopupPTY(t *testing.T) {
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "home", SessionName: "home", Cols: 80, Rows: 24, Shell: restoreShell(t), Restore: true, DisablePublish: true})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	reply := func(marker string, cols, rows int) {
		t.Helper()
		host.Send(marker + "\r")
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmtReply(marker, cols, rows)) })
	}
	host.Send("\x0cc")
	reply("new-local", 80, 24)
	host.Resize(90, 28)
	reply("resized-local", 90, 28)
	host.Send("\x0cp")
	waitRestoreRequest(t, host, 80, 24)
	host.Resize(80, 24)
	reply("original-local", 80, 24)
	host.Send("\x0cn")
	waitRestoreRequest(t, host, 90, 28)
	host.Resize(90, 28)
	reply("returned-local", 90, 28)
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("j hide") })
	reply("popup-box", 90, 13)
	host.Resize(92, 30)
	reply("resized-popup", 92, 14)
	host.Send("\x0cj")
	reply("resized-parent", 92, 30)
}
