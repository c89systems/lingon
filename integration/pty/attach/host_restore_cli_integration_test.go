//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostRestoreCLI(t *testing.T) {
	for _, flag := range []string{"-r", "--restore"} {
		t.Run(flag, func(t *testing.T) {
			h := newHarness(t)
			source := h.StartHost(ptytest.HostOptions{SessionID: "remote", SessionName: "remote", Cols: 100, Rows: 30, Shell: fixedPromptEmitRowsBash(t)})
			t.Cleanup(source.Cancel)
			waitForPromptVisible(t, source, 8*time.Second)
			waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"remote"}, 8*time.Second)
			viewer := startGeometryCLI(t, shortHomeDir(t), flag, "-R", "--session", "local", "--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure", "--shell", fixedPromptEmitRowsBash(t), "--disable-desktop-notifications")
			t.Cleanup(viewer.Cancel)
			wait := func(check func(ptytest.Screen) bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := viewer.WaitForScreen(ctx, check); err != nil {
					t.Fatalf("%v\n%s", err, viewer.Screen().String())
				}
			}
			request := func(cols, rows int) {
				t.Helper()
				var raw string
				want := fmt.Sprintf("\x1b[8;%d;%dt", rows, cols)
				wait(func(_ ptytest.Screen) bool { raw += viewer.DrainRaw(); return strings.Contains(raw, want) })
			}
			reply := func(marker string, cols, rows int) {
				t.Helper()
				viewer.Send(fmt.Sprintf("printf '%s:%%s\\n' \"$(stty size)\"\r", marker))
				wait(func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("%s:%d %d", marker, rows, cols)) })
			}
			wait(func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
			viewer.Send("\x0c/remote\r")
			request(100, 30)
			viewer.Resize(100, 30)
			reply("REMOTE_ACK", 100, 30)
			viewer.Resize(88, 26)
			reply("REMOTE_MANUAL", 88, 26)
			viewer.Send("\x0c/local\r")
			request(80, 24)
			viewer.Resize(80, 24)
			reply("LOCAL_RETURN", 80, 24)
			viewer.Resize(84, 27)
			reply("LOCAL_MANUAL", 84, 27)
			viewer.Send("\x0c/remote\r")
			request(88, 26)
			viewer.Resize(88, 26)
			reply("REMOTE_RETURN", 88, 26)
			viewer.Send("\x0c/local\r")
			request(84, 27)
			viewer.Resize(84, 27)
			reply("LOCAL_NEW_SIZE", 84, 27)
		})
	}
}
