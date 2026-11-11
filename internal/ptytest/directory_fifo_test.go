package ptytest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/sessionmanage"
)

func TestFIFOCompletionDoesNotBlockRelayManagementPTY(t *testing.T) {
	base := t.TempDir()
	fifo := filepath.Join(base, "pipe")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "fifo", SessionName: "fifo", WorkingDirectory: base, Shell: folderShell(t), Cols: 100, Rows: 24})
	t.Cleanup(host.Cancel)
	// Release a regressed blocking open before tearing the publisher down. The
	// writer is never present during assertions; it cannot hide a FIFO-open bug.
	t.Cleanup(func() {
		writer, err := os.OpenFile(fifo, os.O_RDWR|unix.O_NONBLOCK, 0)
		if err == nil {
			_ = writer.Close()
		}
	})
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "fifo")
	client := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), Insecure: true, Clock: clk}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	result, err := client.Manage(ctx, sessionmanage.Request{RequestID: "bad-directory", SessionID: "fifo", Kind: "directory", Path: fifo + "/child"})
	if ctx.Err() != nil || err == nil || result.Error == "" || result.Path != base {
		t.Fatalf("FIFO completion must return a directory error with the current cwd: %+v %v (context: %v)", result, err, ctx.Err())
	}
	result, err = client.Manage(ctx, sessionmanage.Request{RequestID: "rename-after-error", SessionID: "fifo", Kind: "rename", Name: "responsive"})
	if err != nil || result.Name != "responsive" {
		t.Fatalf("management stalled after rejected completion: %+v %v", result, err)
	}
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "responsive") })
}
