package ptytest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestSSHDisconnectEscapeHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "single", "multi"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "capture-ssh-escape")
			script := "#!/bin/sh\nstty raw -echo\nprintf '\\r\\nESCAPE_READY\\r\\n'\nod -An -tx1 -N3\nprintf '\\r\\nCAPTURE_DONE'\nread ignored\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "ssh-escape", Shell: path, Cols: 100, Rows: 20})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ESCAPE_READY") })
			viewer := host
			if mode != "host" {
				waitRestoreHost(t, recorder, "ssh-escape")
				if mode == "single" {
					viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "ssh-escape", Cols: 100, Rows: 20, RequestControl: true})
				} else {
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "ssh-escape", Cols: 100, Rows: 20})
				}
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ESCAPE_READY") })
			}
			viewer.Send("\x0cC")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("0d 7e 2e") && s.Contains("CAPTURE_DONE") })
		})
	}
}

func TestAttachQuitLeavesRemoteRunningPTY(t *testing.T) {
	for _, key := range []string{"\x0cq", "\x04"} {
		t.Run(fmt.Sprintf("%x", key), func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "detach-source", Shell: searchShell(t), Cols: 100, Rows: 20})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			waitRestoreHost(t, recorder, "detach-source")
			viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "detach-source", Cols: 100, Rows: 20})
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			viewer.Send(key)
			if done, err := viewer.WaitErr(5 * time.Second); !done || err != nil {
				t.Fatalf("quit: done=%v err=%v", done, err)
			}
			viewer.Cancel()
			// Fenced input on the original host proves its shell survived detach.
			host.Send("survived-detach\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:survived-detach") })
		})
	}
}
