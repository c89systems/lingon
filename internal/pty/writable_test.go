package pty

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type observedWritableConn struct {
	syscall.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *observedWritableConn) SyscallConn() (syscall.RawConn, error) {
	raw, err := c.Conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	return observedWritableRaw{RawConn: raw, enter: func() { c.once.Do(func() { close(c.entered) }) }}, nil
}

type observedWritableRaw struct {
	syscall.RawConn
	enter func()
}

func (c observedWritableRaw) Write(f func(uintptr) bool) error {
	return c.RawConn.Write(func(fd uintptr) bool { c.enter(); return f(fd) })
}

func TestWaitWritableCancellationUnderBackpressure(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	raw, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fillErr error
	if err := raw.Control(func(fd uintptr) {
		fillErr = unix.SetNonblock(int(fd), true)
		if fillErr != nil {
			return
		}
		chunk := make([]byte, 4096)
		for {
			_, fillErr = unix.Write(int(fd), chunk)
			if errors.Is(fillErr, syscall.EINTR) {
				continue
			}
			if fillErr != nil {
				return
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(fillErr, syscall.EAGAIN) {
		t.Fatalf("fill pipe: %v", fillErr)
	}
	operation, cancel := context.WithCancel(t.Context())
	defer cancel()
	bound, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	conn := &observedWritableConn{Conn: writer, entered: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- WaitWritable(operation, conn) }()
	select {
	case <-conn.entered:
	case <-bound.Done():
		t.Fatal("did not reach native poll")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation=%v", err)
		}
	case <-bound.Done():
		t.Fatal("backpressure hid cancellation")
	}
}

func TestWaitWritableClosedAndReadyDescriptors(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	if err := WaitWritable(t.Context(), writer); err != nil {
		t.Fatalf("empty pipe: %v", err)
	}
	_ = writer.Close()
	if err := WaitWritable(t.Context(), writer); err == nil {
		t.Fatal("closed descriptor accepted")
	}
	if err := WaitWritable(t.Context(), nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("nil descriptor: %v", err)
	}
}
