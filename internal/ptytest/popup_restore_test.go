package ptytest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostPopupResizesOnRestoreAcknowledgementPTY(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "geometry-fifo")
	// Each fixture inherits only its harness-owned child PTY. Keep that descriptor
	// for read-only stty queries; use a FIFO so queries never enter host input and
	// cannot invoke its input-time geometry repair. Live child PIDs fence startup.
	script := fmt.Sprintf(`#!/bin/sh
stty -echo
exec 3<&0
mkfifo '%s/'"$$"
printf '\nCHILD_READY:%%s\n' "$$"
while IFS= read -r marker; do
 printf '\nSIZE:%%s:%%s:%%s\n' "$$" "$marker" "$(stty size <&3)"
done < '%s/'"$$"
`, dir, dir)
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	host := h.StartHost(ptytest.HostOptions{SessionID: "home", Shell: shell, Cols: 80, Rows: 24, Restore: true, DisablePublish: true})
	t.Cleanup(host.Cancel)
	readyPattern := regexp.MustCompile(`CHILD_READY:([0-9]+)`)
	children := make(map[string]bool)
	awaitChild := func() string {
		t.Helper()
		var pid string
		waitSearchScreen(t, host, func(s ptytest.Screen) bool {
			for _, match := range readyPattern.FindAllStringSubmatch(s.String(), -1) {
				if !children[match[1]] {
					pid = match[1]
					children[pid] = true
					return true
				}
			}
			return false
		})
		return pid
	}
	queries := make(map[string]*os.File)
	t.Cleanup(func() {
		for _, fifo := range queries {
			_ = fifo.Close()
		}
	})
	report := func(pid, marker string, cols, rows int) {
		t.Helper()
		fifo := queries[pid]
		if fifo == nil {
			var err error
			fifo, err = os.OpenFile(filepath.Join(dir, pid), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			queries[pid] = fifo
		}
		if _, err := fmt.Fprintln(fifo, marker); err != nil {
			t.Fatal(err)
		}
		prefix := "SIZE:" + pid + ":" + marker + ":"
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(prefix) })
		if !host.Screen().Contains(fmt.Sprintf("%s%d %d", prefix, rows, cols)) {
			t.Fatalf("application PTY differs from restored geometry:\n%s", host.Screen().String())
		}
	}
	first := awaitChild()
	host.Send("\x0cc")
	second := awaitChild()
	host.DrainRaw()
	host.Resize(90, 30)
	var resizedOutput string
	waitSearchScreen(t, host, func(s ptytest.Screen) bool {
		resizedOutput += host.DrainRaw()
		return len(s.Lines) == 30 && resizedOutput != ""
	})
	report(second, "manual-parent", 90, 30)
	host.Send("\x0cj")
	popup := awaitChild()
	report(popup, "initial-popup", 90, 14)
	host.Send("\x0cp")
	waitRestoreRequest(t, host, 80, 24)
	host.Resize(80, 24)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return len(s.Lines) == 24 && !s.Contains("j hide") })
	report(first, "first-parent", 80, 24)
	host.Send("\x0cn")
	waitRestoreRequest(t, host, 90, 30)
	host.Resize(90, 30)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return strings.Contains(s.Row(15), "j hide") })
	report(popup, "restored-popup", 90, 14)
	// Hiding the popup must reveal the parent's preserved dimensions, not its box.
	host.Send("\x0cj")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return !s.Contains("j hide") })
	report(second, "preserved-parent", 90, 30)
}
