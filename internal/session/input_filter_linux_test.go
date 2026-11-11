//go:build linux

package session

import (
	"bytes"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestRemoteInputEOFGuardPreservesApplicationTermios(t *testing.T) {
	for _, tc := range []struct {
		name      string
		canonical bool
		veof      byte
		want      []byte
	}{
		{"canonical", true, 4, []byte{'a', 'b'}},
		{"raw", false, 4, []byte{'a', 4, 'b'}},
		{"disabled-by-app", true, 0, []byte{'a', 4, 'b'}},
		{"custom-app-key", true, 17, []byte{'a', 4, 'b'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
			if err := withTTYDescriptor(slave, func(fd int) error {
				settings, err := unix.IoctlGetTermios(fd, unix.TCGETS)
				if err != nil {
					return err
				}
				if tc.canonical {
					settings.Lflag |= unix.ICANON
				} else {
					settings.Lflag &^= unix.ICANON
				}
				settings.Cc[unix.VEOF] = tc.veof
				return unix.IoctlSetTermios(fd, unix.TCSETS, settings)
			}); err != nil {
				t.Fatal(err)
			}
			before, err := readTermios(slave)
			if err != nil {
				t.Fatal(err)
			}
			if got := filterRemoteInput(slave, []byte{'a', 4, 'b'}); !bytes.Equal(got, tc.want) {
				t.Fatalf("filtered=%v, want %v", got, tc.want)
			}
			after, err := readTermios(slave)
			if err != nil || before != after {
				t.Fatalf("input filtering changed application termios: %v", err)
			}
		})
	}
}
