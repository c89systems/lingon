package ptytest_test

import (
	"os"
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachForceCloseUnresponsiveApplicationPTY(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "unresponsive-shell")
	// The application neither reads stdin nor exits on EOF, HUP or TERM.
	// It runs only inside StartHost's dedicated child PTY.
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ntrap '' HUP TERM\nprintf '\\nBLOCKED_READY\\n'\nexec sleep 300\n"), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	victim := h.StartHost(ptytest.HostOptions{SessionID: "a-blocked", Shell: shell, Cols: 100, Rows: 20})
	t.Cleanup(victim.Cancel)
	survivor := h.StartHost(ptytest.HostOptions{SessionID: "b-survivor", Shell: searchShell(t), Cols: 100, Rows: 20})
	t.Cleanup(survivor.Cancel)
	waitSearchScreen(t, victim, func(s ptytest.Screen) bool { return s.Contains("BLOCKED_READY") })
	waitSearchScreen(t, survivor, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "a-blocked")
	waitRestoreHost(t, recorder, "b-survivor")
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "a-blocked", Cols: 100, Rows: 20})
	t.Cleanup(viewer.Cancel)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("BLOCKED_READY") })
	viewer.Send("\x0cd\x0ch")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return s.Contains("Copyright (C) 2026 C89 Systems AB") && s.Contains("Ctrl+L h  help")
	})
	viewer.Send("q\x0cQ")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") && !s.Contains("BLOCKED_READY") })
	viewer.Send("survived-force-close\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:survived-force-close") })
}
