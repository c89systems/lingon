package sessionmanage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDirectoryCompletionRejectsFIFO(t *testing.T) {
	for _, mode := range []string{"fifo", "symlink", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			fifo := filepath.Join(base, "pipe")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			target := fifo
			if mode == "symlink" {
				target = filepath.Join(base, "link")
				if err := os.Symlink(fifo, target); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			type response struct {
				result Result
				err    error
			}
			done := make(chan response, 1)
			go func() {
				result, err := NewDirectory(base).Complete(ctx, target+"/child")
				done <- response{result, err}
			}()
			// This deadline is only a deadlock watchdog. No timing or sleeps synchronize
			// the operation. On failure, opening a nonblocking writer releases old code
			// so the test never leaves a syscall or goroutine behind.
			bound, stop := context.WithTimeout(t.Context(), 8*time.Second)
			defer stop()
			var got response
			select {
			case got = <-done:
			case <-bound.Done():
				writer, err := os.OpenFile(fifo, os.O_RDWR|unix.O_NONBLOCK, 0)
				if err != nil {
					t.Fatal(err)
				}
				got = <-done
				_ = writer.Close()
				t.Fatal("directory completion blocked opening a FIFO")
			}
			want := error(unix.ENOTDIR)
			if mode == "cancelled" {
				want = context.Canceled
			}
			if !errors.Is(got.err, want) || got.result.Path != base || len(got.result.Directories) != 0 {
				t.Fatalf("completion: %+v %v; want %v with unchanged cwd", got.result, got.err, want)
			}
		})
	}
}
