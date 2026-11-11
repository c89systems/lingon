package ptytest_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestOSC8HyperlinksHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "single-attach", "multi-attach"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "links-shell")
			script := `#!/bin/sh
printf '\nLINK-READY\n'
while IFS= read -r line; do
 uri='https://example.invalid/ventures'
 params='id=ventures'
 case "$line" in change) uri='https://example.invalid/changed';; identity) uri='https://example.invalid/changed'; params='id=other';; esac
 if [ "$line" = plain ]; then
 printf '\033]8;;\033\\\033[4;1H\033[4;34mChalmers Ventures\033[0m normal\033[K'
 else
 printf '\033[4;1H\033]8;%s;%s\033\\\033[4;34mChalmers Ventures\033[0m\033]8;;\033\\ normal\033[K' "$params" "$uri"
 fi
 printf '\033[5;1H\033[4;34mordinary underline\033[0m\033[K\033[7;1HDONE:%s\033[K' "$line"
done
`
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			var sourceMu sync.Mutex
			var source strings.Builder
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{SessionID: "links", Shell: path, Cols: 80, Rows: 16, OnPTYRead: func(data []byte) { sourceMu.Lock(); source.Write(data); sourceMu.Unlock() }})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("LINK-READY") })
			host.DrainRaw()
			host.Send("paint\r")
			var raw string
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { raw += host.DrainRaw(); return s.Contains("DONE:paint") })
			sourceMu.Lock()
			fromApp := source.String()
			sourceMu.Unlock()
			if !strings.Contains(fromApp, "\x1b]8;id=ventures;https://example.invalid/ventures") {
				t.Fatal("fixture did not emit an actual OSC 8 hyperlink")
			}
			viewer := host
			if mode != "host" {
				waitRestoreHost(t, recorder, "links")
				if mode == "single-attach" {
					viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "links", RequestControl: true, Cols: 80, Rows: 16})
				} else {
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "links", Cols: 80, Rows: 16})
				}
				t.Cleanup(viewer.Cancel)
				raw = ""
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return s.Contains("DONE:paint") })
			}
			if !strings.Contains(raw, "\x1b]8;id=ventures;https://example.invalid/ventures") {
				t.Fatal("actual OSC 8 target was lost between application output and terminal rendering")
			}
			// The same blue, underlined appearance alone must never make text a link.
			snap := viewer.Snapshot()
			if snap.Cells[3*80].Hyperlink != "id=ventures;https://example.invalid/ventures" || snap.Cells[4*80].Hyperlink != "" {
				t.Fatal("hyperlink metadata lost or inferred from ordinary underline")
			}
			if snap.Cells[4*80].Mode&terminal.ModeUnderline == 0 {
				t.Fatal("ordinary underline styling lost")
			}
			for _, step := range []struct{ command, uri, params string }{{"change", "https://example.invalid/changed", "id=ventures"}, {"identity", "https://example.invalid/changed", "id=other"}} {
				viewer.DrainRaw()
				raw = ""
				viewer.Send(step.command + "\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { raw += viewer.DrainRaw(); return s.Contains("DONE:" + step.command) })
				if !strings.Contains(raw, "\x1b]8;"+step.params+";"+step.uri) {
					t.Fatalf("hyperlink-only update lost: %s", step.command)
				}
			}
			viewer.Send("\x0ch")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("Copyright") })
			if viewer.Snapshot().Cells[3*80+15].Hyperlink != "" {
				t.Fatal("help chrome inherited application hyperlink")
			}
			viewer.DrainRaw()
			raw = ""
			viewer.Send("q")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
				raw += viewer.DrainRaw()
				return s.Contains("DONE:identity") && !s.Contains("Copyright")
			})
			if !strings.Contains(raw, "https://example.invalid/changed") {
				t.Fatal("help redraw lost hyperlink target")
			}
			if mode != "single-attach" {
				otherPath := filepath.Join(t.TempDir(), "plain-shell")
				otherScript := "#!/bin/sh\nprintf '\\033[4;1H\\033[4;34mChalmers Ventures\\033[0m\\033[7;1HOTHER_READY'\nwhile IFS= read -r line; do :; done\n"
				if err := os.WriteFile(otherPath, []byte(otherScript), 0700); err != nil {
					t.Fatal(err)
				}
				other := h.StartHost(ptytest.HostOptions{SessionID: "plain", Shell: otherPath, Cols: 80, Rows: 16})
				t.Cleanup(other.Cancel)
				waitSearchScreen(t, other, func(s ptytest.Screen) bool { return s.Contains("OTHER_READY") })
				waitRestoreHost(t, recorder, "plain")
				viewer.Send("\x0c?")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "plain") })
				viewer.Send("plain\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("OTHER_READY") })
				if viewer.Snapshot().Cells[3*80].Hyperlink != "" {
					t.Fatal("tab switch inherited another session's target")
				}
				viewer.Send("\x0c?links\r")
				raw = ""
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					raw += viewer.DrainRaw()
					return s.Contains("DONE:identity") && !s.Contains("OTHER_READY")
				})
				if !strings.Contains(raw, "https://example.invalid/changed") {
					t.Fatal("returning to a tab lost its target")
				}
			}
			viewer.Send("plain\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("DONE:plain") })
			cell := viewer.Snapshot().Cells[3*80]
			if cell.Hyperlink != "" || cell.Mode&terminal.ModeUnderline == 0 {
				t.Fatalf("same-text link removal failed: %+v", cell)
			}
		})
	}
}
