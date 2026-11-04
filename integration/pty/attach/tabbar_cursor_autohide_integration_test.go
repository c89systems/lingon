//go:build integration

package integrationptyattach_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func waitTabCursorScreen(t *testing.T, viewer *ptytest.PTYSession, check func(ptytest.Screen) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if err := viewer.WaitForScreen(ctx, check); err != nil {
		t.Fatalf("tab cursor screen: %v\n%s", err, viewer.Screen().String())
	}
}

func TestTabBarHidesForRowOneCursorAfterSwitchPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach", "cli-attach"} {
		for _, move := range []string{"readline-clear", "application-home"} {
			t.Run(mode+"/"+move, func(t *testing.T) {
				clk := clock.NewMock()
				h := newHarness(t, ptytest.WithClock(clk))
				host := h.StartHost(ptytest.HostOptions{SessionID: "row-one", Shell: fixedPromptEmitRowsBash(t), Cols: 100, Rows: 20})
				t.Cleanup(host.Cancel)
				waitTabCursorScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
				host.Send("printf 'FIRST_%s\\n' PARENT\r")
				waitTabCursorScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("FIRST_PARENT") })
				host.Send("\x0cc")
				waitTabCursorScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") && !s.Contains("FIRST_PARENT") })
				host.Send("printf 'SECOND_%s\\n' PARENT\r")
				waitTabCursorScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") })
				viewer := host
				if mode != "host" {
					waitForSessionsWithTimeout(t, clk, h.Endpoint(), h.AccessToken(), []string{"row-one", "row-one-2"}, 8*time.Second)
					if mode == "cli-attach" {
						viewer = startLingonAttachCLI(t, h, "row-one", 100, 20)
					} else {
						viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "row-one", Cols: 100, Rows: 20})
					}
					t.Cleanup(viewer.Cancel)
				} else {
					host.Send("\x0cn")
				}
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FIRST_PARENT") && !s.Contains("SECOND_PARENT") })
				// Prime both views so the final switch reuses a cached connection.
				viewer.Send("\x0cn")
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") && !s.Contains("FIRST_PARENT") })
				viewer.Send("\x0cp")
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FIRST_PARENT") && !s.Contains("SECOND_PARENT") })
				viewer.Send("\x0ct")
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("theme:") })
				viewer.Send("\x0cn")
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") && !s.Contains("FIRST_PARENT") })
				if move == "readline-clear" {
					viewer.Send("\x0c\x0c")
				} else {
					// Cursor movement comes from application output, without Ctrl+L.
					viewer.Send("printf '\\033[2J\\033[H'\r")
				}
				// In-process variants freeze mock time and require the active banner.
				// The CLI variant exercises its ordinary process clock.
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool {
					return strings.HasPrefix(s.Row(0), "PROMPT>") && viewer.Cursor().Row == 1 && !s.Contains("SECOND_PARENT") && (mode == "cli-attach" || s.Contains("theme:"))
				})
				viewer.Send("visible")
				waitTabCursorScreen(t, viewer, func(s ptytest.Screen) bool { return strings.HasPrefix(s.Row(0), "PROMPT>visible") })
			})
		}
	}
}
