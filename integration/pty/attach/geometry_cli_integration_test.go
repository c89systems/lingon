//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func startGeometryCLI(t *testing.T, home string, args ...string) *ptytest.PTYSession {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, 80, 24)
	fd, err := syscall.Dup(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	resize := os.NewFile(uintptr(fd), "geometry-cli-resize")
	terminal := ptytest.NewPTYSession(t, master, resize, 80, 24)
	t.Cleanup(terminal.Close)
	cmd := exec.Command(buildLingonAttachBinary(t), args...)
	cmd.Env = testAttachCLIEnv(home)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	go func() { <-terminal.Context().Done(); _ = cmd.Process.Kill() }()
	go func() {
		err := cmd.Wait()
		if terminal.Context().Err() != nil {
			err = nil
		}
		terminal.SetRunErr(err)
	}()
	return terminal
}

func TestGeometryHostAndAttachCLI(t *testing.T) {
	for _, tc := range []struct {
		name       string
		attach     bool
		flags      []string
		cols, rows int
	}{
		{"host-geometry", false, []string{"-g", "100x50"}, 100, 50},
		{"host-dimensions", false, []string{"--cols", "100", "--rows", "50"}, 100, 50},
		{"attach-geometry", true, []string{"-g", "100x50"}, 100, 50},
		{"attach-dimensions", true, []string{"--cols", "100", "--rows", "50"}, 100, 50},
		{"attach-geometry-then-restore", true, []string{"-r", "-g", "100x50", "--cols", "120", "--rows", "60"}, 100, 50},
		{"attach-dimensions-then-restore", true, []string{"-r", "--cols", "100", "--rows", "50"}, 100, 50},
		{"attach-cols-then-restore", true, []string{"--restore", "--cols", "100"}, 100, 24},
		{"attach-rows-then-restore", true, []string{"-r", "--rows", "50"}, 80, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := shortHomeDir(t)
			cleanupManagedTerminals(t, filepath.Join(home, lingon.DefaultConfigDirName))
			args := []string{"--offline", "--token", "geometry-fixture", "--session", "geometry-cli", "--shell", fixedPromptEmitRowsBash(t)}
			if tc.attach {
				cmd := exec.Command(buildLingonAttachBinary(t), append([]string{"-x", "-g", "100x30"}, args...)...)
				cmd.Env = testAttachCLIEnv(home)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("headless startup: %v %s", err, output)
				}
				args = []string{"a", "-x", "geometry-cli", "--request-control", "--disable-desktop-notifications"}
			}
			terminal := startGeometryCLI(t, home, append(args, tc.flags...)...)
			t.Cleanup(terminal.Cancel)
			wait := func(check func(ptytest.Screen) bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := terminal.WaitForScreen(ctx, check); err != nil {
					t.Fatalf("%v\n%s", err, terminal.Screen().String())
				}
			}
			var raw string
			sequence := fmt.Sprintf("\x1b[8;%d;%dt", tc.rows, tc.cols)
			wait(func(_ ptytest.Screen) bool { raw += terminal.DrainRaw(); return strings.Contains(raw, sequence) })
			terminal.Send("printf 'CLI_SIZE:%s\\n' \"$(stty size)\"\r")
			want := fmt.Sprintf("CLI_SIZE:%d %d", tc.rows, tc.cols)
			wait(func(s ptytest.Screen) bool { return s.Contains(want) })
		})
	}
}
