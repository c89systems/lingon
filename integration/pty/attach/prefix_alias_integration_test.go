//go:build integration

package integrationptyattach_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestControlAliasesHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			recorder := ptytest.NewWSRecorder()
			h := newHarness(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			cfg := shortHomeDir(t)
			cleanupManagedTerminals(t, cfg)
			host := h.StartHost(ptytest.HostOptions{SessionID: "aliases", Shell: fixedPromptEmitRowsBash(t), Cols: 100, Rows: 20,
				ConfigDir: cfg, HeadlessExecutable: buildLingonAttachBinary(t)})
			t.Cleanup(host.Cancel)
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") })
			host.Send("printf 'FIRST_%s\\n' PARENT\r")
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("FIRST_PARENT") })
			host.Send("\x0cc")
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("PROMPT>") && !s.Contains("FIRST_PARENT") })
			host.Send("printf 'SECOND_%s\\n' PARENT\r")
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") })
			viewer := host
			if mode == "attach" {
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
					var frame protocolpb.Frame
					if r.Role != "host" || r.Direction != ptytest.DirServerToClient || proto.Unmarshal(r.Raw, &frame) != nil {
						return false
					}
					var first, second bool
					for _, session := range frame.GetSessions().GetSessions() {
						first = first || session.Id == "aliases"
						second = second || session.Id == "aliases-2"
					}
					return first && second
				}); err != nil {
					t.Fatal(err)
				}
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "aliases-2", Cols: 100, Rows: 20})
				t.Cleanup(viewer.Cancel)
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") })
			}
			viewer.Send("\x0c\x0e")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("FIRST_PARENT") && !s.Contains("SECOND_PARENT") })
			viewer.Send("\x0c\x10")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") && !s.Contains("FIRST_PARENT") })
			viewer.Send("\x0c\x0a")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("j hide") && strings.Contains(strings.Join(s.Lines[11:], "\n"), "PROMPT>")
			})
			child := strings.Fields(viewer.Screen().Row(10))[0]
			viewer.Send("printf 'POPUP_%s\\n' ALIAS\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIAS") })
			viewer.Send("\x0c\x0a")
			popupScreen(t, viewer, func(s ptytest.Screen) bool {
				return s.Contains("SECOND_PARENT") && !s.Contains("j hide") && !s.Contains("POPUP_ALIAS")
			})
			viewer.Send("\x0c\x0a")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("POPUP_ALIAS") && strings.Contains(s.Row(10), child) })
			// The original shortcut still toggles the same popup.
			viewer.Send("\x0cj")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("SECOND_PARENT") && !s.Contains("j hide") })
			viewer.Send("\x0c\x14")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("theme:") && s.Contains("SECOND_PARENT") })
		})
	}
}
