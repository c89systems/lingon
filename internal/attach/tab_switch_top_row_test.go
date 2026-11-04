package attach_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestAttachSwitchToAlreadyTopRowHidesTabsPTY(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"top-a", "top-b"} {
		shell := filepath.Join(t.TempDir(), "top-row-shell")
		script := fmt.Sprintf("#!/bin/sh\nstty -echo\nprintf '\\033[H%s_PROMPT> '\nwhile IFS= read -r line; do :; done\n", id)
		if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		connected := make(chan struct{})
		var once sync.Once
		host := h.StartHost(ptytest.HostOptions{SessionID: id, Shell: shell, Cols: 100, Rows: 20,
			OnPublishStatus: func(status session.PublishStatus) {
				if status.Kind == session.PublishStatusConnected {
					once.Do(func() { close(connected) })
				}
			}})
		t.Cleanup(host.Cancel)
		awaitTabSwitchEvent(t, connected)
	}
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "top-a", Cols: 100, Rows: 20})
	t.Cleanup(viewer.Cancel)
	waitTabSwitchFrame(t, viewer, "top-a_PROMPT>", "top-b_PROMPT>")
	for _, id := range []string{"top-b", "top-a", "top-b"} {
		viewer.Send("\x0cn")
		// No application output, extra key or time advance can uncover this row.
		waitTabSwitchFrame(t, viewer, id+"_PROMPT>", "")
		if cursor := viewer.Cursor(); cursor.Row != 1 {
			t.Fatalf("tab %s: cursor=%v, want row 1", id, cursor)
		}
	}
}
