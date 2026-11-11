package pty

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"pkt.systems/lingon/internal/terminal"
)

// Start launches the command attached to a new PTY.
func Start(cmd *exec.Cmd) (*os.File, error) {
	return pty.Start(cmd)
}

// StartWithTTY sets an owned PTY's geometry before starting the command and
// returns both master and slave. Programs can query the correct size immediately.
func StartWithTTY(cmd *exec.Cmd, size terminal.WindowSize) (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	if err := Resize(master, size.Cols, size.Rows); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if cmd.Stdin == nil {
		cmd.Stdin = slave
	}
	if cmd.Stdout == nil {
		cmd.Stdout = slave
	}
	if cmd.Stderr == nil {
		cmd.Stderr = slave
	}
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

// Resize updates the PTY window size.
func Resize(ptyFile *os.File, cols, rows int) error {
	if ptyFile == nil {
		return nil
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	raw, err := ptyFile.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	// Control keeps the descriptor alive through the ioctl without changing its
	// blocking mode. File.Fd does not provide that protection against Close.
	if err := raw.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	}); err != nil {
		return err
	}
	return resizeErr
}
