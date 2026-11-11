package ptytest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestParentApplicationClearPreservesPopupWithoutFlickerPTY(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "clear-fixture")
	script := "#!/bin/sh\nprintf '\\nREADY> '\nwhile IFS= read -r line; do\n if [ \"$line\" = CLEAR ]; then printf '\\033[2J\\033[H\\nCLEARED_BASE\\nREADY> '; else printf '\\nREPLY:%s\\nREADY> ' \"$line\"; fi\ndone\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "clear-base", Shell: shell, Cols: 100, Rows: 20})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "clear-base")
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
	})
	host.Send("popup-stays\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-stays") })
	controller := h.StartAttach(ptytest.AttachOptions{SessionID: "clear-base", Cols: 100, Rows: 20, RequestControl: true})
	t.Cleanup(controller.Cancel)
	waitSearchScreen(t, controller, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.DrainRaw()
	controller.Send("CLEAR\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("CLEARED_BASE") && s.Contains("REPLY:popup-stays") })
	if strings.Contains(host.DrainRaw(), "\x1b[2J") {
		t.Fatal("the parent application's clear erased the physical popup before repaint")
	}
}
