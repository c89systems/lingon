package ptytest_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestApplicationClearDoesNotErasePinnedHostHistoryPTY(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "clear-input")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	input, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	shell := filepath.Join(dir, "shell")
	source := "#!/bin/sh\nprintf '\\n'\ni=0\nwhile [ $i -lt 100 ]; do i=$((i + 1)); printf 'ROW-%05d\\n' \"$i\"; done\nwhile IFS= read -r command; do\n printf '\\033[2J\\033[HREMOTE-RESET\\n\\033]0;HISTORY-CLEAR-DONE\\007'\ndone < '" + strings.ReplaceAll(fifo, "'", "'\\''") + "'\n"
	if err := os.WriteFile(shell, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "clear-pinned", SessionName: "clear-pinned", Shell: shell, Cols: 60, Rows: 12})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("ROW-00100") })
	waitRestoreHost(t, recorder, "clear-pinned")
	host.Send("\x0c[g")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[0%]") })
	before := host.Snapshot()
	host.DrainRaw()
	if _, err := input.WriteString("clear\n"); err != nil {
		t.Fatal(err)
	}
	// The changed OSC title is an output barrier even though the viewed history
	// cells and percentage are unchanged. No wall-clock delay establishes proof.
	waitSearchScreen(t, host, func(_ ptytest.Screen) bool { return host.Snapshot().Title == "HISTORY-CLEAR-DONE" })
	after := host.Snapshot()
	if !reflect.DeepEqual(before.Cells[60:], after.Cells[60:]) {
		t.Fatal("application clear changed pinned historical cells")
	}
	if raw := host.DrainRaw(); strings.Contains(raw, "\x1b[2J") || strings.Contains(raw, "ROW-") {
		t.Fatalf("application clear repainted pinned history: %q", raw)
	}
}
