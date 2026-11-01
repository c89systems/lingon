package ptytest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func searchShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '\\nREADY> '\nwhile IFS= read -r line; do printf '\\nREPLY:%s\\nREADY> ' \"$line\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitSearchScreen(t *testing.T, terminal *ptytest.PTYSession, check func(ptytest.Screen) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := terminal.WaitForScreen(ctx, check); err != nil {
		t.Fatalf("screen: %v\n%s", err, terminal.Screen().String())
	}
}

func searchField(t *testing.T, terminal *ptytest.PTYSession, want string) {
	t.Helper()
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(s.Rows-1)) == want })
}

func TestSessionSearchHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			clk := clock.NewMock()
			h := ptytest.New(t, ptytest.WithClock(clk))
			host := h.StartHost(ptytest.HostOptions{SessionID: "search-a", SessionName: "alpha", Shell: searchShell(t), Cols: 80, Rows: 16})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			host.Send("origin\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:origin") })
			// Create a second real local PTY, then name it. A reply fences startup.
			host.Send("\x0cc")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool {
				return s.Contains("alpha-2") && s.Contains("READY>") && !s.Contains("REPLY:origin")
			})
			host.Send("second\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:second") })
			host.Send("\x0cealpine\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("alpine") && !s.Contains("Rename session") })
			terminal := host
			if mode == "attach" {
				terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "search-a", Cols: 80, Rows: 16})
				t.Cleanup(terminal.Cancel)
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			}
			terminal.Send("\x0c/al")
			searchField(t, terminal, "/ alpha")
			waitSearchScreen(t, terminal, func(_ ptytest.Screen) bool { return terminal.Cursor().Col == 5 })
			// The entered prefix and ghost suffix must have distinct foregrounds.
			snap := terminal.Snapshot()
			if snap.Cells[15*80+3].FG == snap.Cells[15*80+4].FG {
				t.Fatal("ghost is not gray")
			}
			terminal.Send("\t")
			searchField(t, terminal, "/ alpha")
			// A full completion cycles to the next session instead of becoming stuck.
			terminal.Send("\t")
			searchField(t, terminal, "/ alpine")
			terminal.Send("\rselected\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:selected") && !strings.HasPrefix(s.Row(s.Rows-1), "/ ")
			})
			// Opening and cycling never activates another tab. Escape keeps alpine.
			terminal.Send("\x0c/\t")
			searchField(t, terminal, "/ alpha")
			terminal.Send("\x1bcancelled\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:cancelled") && s.Contains("REPLY:selected") })
			// Enter accepts ghost completion directly, without a preceding Tab.
			terminal.DrainRaw()
			terminal.Send("\x0c/alph\rfirst\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:first") && !s.Contains("REPLY:selected") })
			if raw := terminal.DrainRaw(); strings.Contains(raw, "\x1b[2J") {
				t.Fatal("search selection cleared the whole screen")
			}
			// No match stays modal; Escape must not leak any search bytes into input.
			terminal.Send("\x0c/unknown\r")
			searchField(t, terminal, "/ unknown")
			terminal.Send("\x1bclean\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:clean") && !s.Contains("REPLY:unknown") })
		})
	}
}
