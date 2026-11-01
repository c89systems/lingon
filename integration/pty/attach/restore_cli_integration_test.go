//go:build integration

package integrationptyattach_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRestoreLocalCLI(t *testing.T) {
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	const id = "restore-local"
	cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--offline", "--session", id, "--geometry", "100x30", "--shell", fixedPromptEmitRowsBash(t))
	cmd.Env = testAttachCLIEnv(home)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start headless: %v %s", err, output)
	}
	terminal := startLingonAttachHeadlessCLI(t, home, id, 80, 24, "-r")
	t.Cleanup(terminal.Cancel)
	wait := func(check func(ptytest.Screen) bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		if err := terminal.WaitForScreen(ctx, check); err != nil {
			t.Fatalf("wait: %v\n%s", err, terminal.Screen().String())
		}
	}
	var raw string
	wait(func(_ ptytest.Screen) bool {
		raw += terminal.DrainRaw()
		return strings.Contains(raw, "\x1b[8;30;100t")
	})
	terminal.Send("printf 'LOCAL_SIZE:%s\\n' \"$(stty size)\"\r")
	wait(func(s ptytest.Screen) bool { return s.Contains("LOCAL_SIZE:30 100") })
	terminal.Send("\x0c/\t")
	wait(func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(s.Rows-1)) == "/ restore-local" })
	terminal.Send("\r\x0c!")
	wait(func(s ptytest.Screen) bool { return s.Contains("Restore off") })
	terminal.Send("printf 'NORMAL_SIZE:%s\\n' \"$(stty size)\"\r")
	wait(func(s ptytest.Screen) bool { return s.Contains("NORMAL_SIZE:24 80") })
}
