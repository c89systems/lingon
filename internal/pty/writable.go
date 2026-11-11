package pty

import (
	"context"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// WaitWritable waits for native descriptor readiness independently of model time.
// Each bounded poll protects the descriptor against closure/reuse and lets
// cancellation interrupt backpressure without changing the descriptor's flags.
func WaitWritable(ctx context.Context, conn syscall.Conn) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if conn == nil {
		return os.ErrInvalid
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	fds := []unix.PollFd{{Events: unix.POLLOUT}}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var pollErr error
		if err := raw.Write(func(fd uintptr) bool {
			fds[0].Fd = int32(fd)
			_, pollErr = unix.Poll(fds, 50)
			return true
		}); err != nil {
			return err
		}
		if pollErr == syscall.EINTR {
			continue
		}
		if pollErr != nil {
			return pollErr
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return syscall.EBADF
		}
		if fds[0].Revents&(unix.POLLOUT|unix.POLLERR|unix.POLLHUP) != 0 {
			return ctx.Err()
		}
	}
}
