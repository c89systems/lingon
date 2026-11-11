package ptytest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRemoteParentPopupResizesBeforeBatchedInputPTY(t *testing.T) {
	for _, restore := range []bool{false, true} {
		for _, route := range []string{"search", "next", "previous"} {
			t.Run(fmt.Sprintf("restore=%v/%s", restore, route), func(t *testing.T) {
				shell := filepath.Join(t.TempDir(), "geometry-shell")
				// This shell, including stty, runs only in harness-owned child PTYs.
				script := "#!/bin/sh\nstty -echo\nprintf '\\nREADY>\\n'\nwhile IFS= read -r line; do printf '\\nREPLY:%s SIZE:%s\\nREADY>\\n' \"$line\" \"$(stty size)\"; done\n"
				if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
				remote := h.StartHost(ptytest.HostOptions{SessionID: "alpha", SessionName: "alpha", Shell: shell, Cols: 80, Rows: 20, AllowRemoteResize: true})
				t.Cleanup(remote.Cancel)
				waitSearchScreen(t, remote, func(s ptytest.Screen) bool { return s.Contains("READY>") })
				host := h.StartHost(ptytest.HostOptions{SessionID: "zhome", SessionName: "zhome", Shell: shell, Cols: 80, Rows: 20, Restore: restore})
				t.Cleanup(host.Cancel)
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") && strings.Contains(s.Row(0), "alpha") })
				host.Send("\x0c/alpha\rremote-first\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:remote-first SIZE:20 80") })
				host.Send("\x0cj")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool {
					return strings.Contains(s.Row(10), "j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "READY>")
				})
				host.Send("popup-first\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-first SIZE:9 80") })
				host.Send("\x0c/zhome\rlocal-first\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:local-first") && !s.Contains("j hide") })
				host.Resize(90, 30)
				host.Send("local-resized\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:local-resized SIZE:30 90") })
				keys := "\x0c/alpha\r"
				if route == "next" {
					keys = "\x0cn"
				}
				if route == "previous" {
					keys = "\x0cp"
				}
				// A later input read repairs stale popup geometry. Keep the switch and
				// the size query in one batch and assert the first application reply.
				host.Send(keys + "popup-return\r")
				waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:popup-return SIZE:") })
				if !host.Screen().Contains("REPLY:popup-return SIZE:14 90") {
					t.Fatalf("remote-parent activation left stale popup geometry:\n%s", host.Screen().String())
				}
				if !strings.Contains(host.Screen().Row(15), "j hide") {
					t.Fatal("popup lost its associated parent or focus")
				}
			})
		}
	}
}
