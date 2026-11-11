package session

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"pkt.systems/lingon/internal/clock"
)

type outputRetryClock struct {
	clock.Clock
	cancel context.CancelFunc
}

func (c outputRetryClock) After(time.Duration) <-chan time.Time {
	c.cancel() // Native I/O readiness must never require model time to advance.
	return nil
}

type gatedOutputFile struct {
	*os.File
	blocked, release chan struct{}
	once             sync.Once
}

func (w *gatedOutputFile) Write(data []byte) (int, error) {
	raw, err := w.SyscallConn()
	if err != nil {
		return 0, err
	}
	var n int
	var writeErr error
	if err := raw.Write(func(fd uintptr) bool { n, writeErr = unix.Write(int(fd), data); return true }); err != nil {
		return 0, err
	}
	if errors.Is(writeErr, syscall.EAGAIN) {
		w.once.Do(func() { close(w.blocked); <-w.release })
	}
	return max(0, n), writeErr
}

func TestOutputBackpressureUsesNativeReadinessWithoutModelTime(t *testing.T) {
	// Own both pipe ends. No inherited terminal, PTY, descriptor or signal is used.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	raw, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var setupErr error
	if err := raw.Control(func(fd uintptr) { setupErr = unix.SetNonblock(int(fd), true) }); err != nil {
		t.Fatal(err)
	}
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	filled := 0
	chunk := make([]byte, 4096)
	for {
		var n int
		var writeErr error
		if err := raw.Write(func(fd uintptr) bool { n, writeErr = unix.Write(int(fd), chunk); return true }); err != nil {
			t.Fatal(err)
		}
		filled += max(n, 0)
		if errors.Is(writeErr, syscall.EINTR) {
			continue
		}
		if errors.Is(writeErr, syscall.EAGAIN) {
			break
		}
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	output := &gatedOutputFile{File: writer, blocked: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { cancel(); release.Do(func() { close(output.release) }) })
	result := make(chan error, 1)
	go func() {
		result <- writeAll(ctx, output, []byte("ready"), outputRetryClock{Clock: clock.NewMock(), cancel: cancel})
	}()
	select {
	case <-output.blocked:
	case <-ctx.Done():
		t.Fatal("write never reached observed backpressure")
	}
	// The first failed write is held until the pipe is physically writable. This
	// proves readiness, without a sleep or scheduling assumption about the writer.
	if _, err := io.ReadFull(reader, make([]byte, filled)); err != nil {
		t.Fatal(err)
	}
	release.Do(func() { close(output.release) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("native output required model time: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("write did not resume after draining")
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "ready" {
		t.Fatalf("output=%q", got)
	}
}
