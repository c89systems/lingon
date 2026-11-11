package ptytest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestStyledUnderlinesHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "underline-shell")
			// The application emits the same SGR style/color used for Neovim SpellBad,
			// and a real single underline next to it. It runs only in the owned sub-PTY.
			script := `#!/bin/sh
printf '\nUNDERLINE-READY\n'
while IFS= read -r line; do
 printf '\033[4;1H\033[0;38;2;224;224;224;48;2;38;34;48m\033[4:3m\033[58:2::215:135:95mcai\033[24;59m \033[4mplain\033[24m normal\033[0m\033[6;1HUNDERLINE-DONE\n'
done
`
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			rec := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(rec))
			host := h.StartHost(ptytest.HostOptions{SessionID: "underlines", Shell: path, Cols: 80, Rows: 16})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("UNDERLINE-READY") })
			viewer := host
			if mode == "attach" {
				waitRestoreHost(t, rec, "underlines")
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "underlines", Cols: 80, Rows: 16})
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("UNDERLINE-READY") })
			}
			viewer.DrainRaw()
			viewer.Send("draw\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("UNDERLINE-DONE") })
			raw := viewer.DrainRaw()
			if !strings.Contains(raw, "4:3") {
				t.Fatalf("curly spelling underline flattened: %q", raw)
			}
			if !strings.Contains(raw, "58;2;215;135;95") && !strings.Contains(raw, "58:2::215:135:95") {
				t.Fatalf("spelling underline color lost: %q", raw)
			}
			snap := viewer.Snapshot()
			for _, x := range []int{0, 1, 2, 4, 5, 6, 7, 8} {
				cell := snap.Cells[3*80+x]
				if cell.Mode&terminal.ModeUnderline == 0 {
					t.Fatalf("underline missing at col %d: %+v", x, cell)
				}
				if cell.FG != terminal.ColorTrue|0xe0e0e0 || cell.BG != terminal.ColorTrue|0x262230 || cell.Mode&terminal.ModeFaint != 0 {
					t.Fatalf("underline color corrupted text attributes: %+v", cell)
				}
			}
			for x := 0; x < 3; x++ {
				if snap.Cells[3*80+x].Mode != terminal.ModeUnderlineCurly || snap.Cells[3*80+x].UnderlineColor != terminal.ColorTrue|0xd7875f {
					t.Fatalf("spelling attributes lost: %+v", snap.Cells[3*80+x])
				}
			}
			for x := 4; x < 9; x++ {
				if snap.Cells[3*80+x].Mode != terminal.ModeUnderline || snap.Cells[3*80+x].UnderlineColor != 0 {
					t.Fatalf("ordinary underline changed: %+v", snap.Cells[3*80+x])
				}
			}
			if snap.Cells[3*80+10].Mode&terminal.ModeUnderline != 0 {
				t.Fatal("underline leaked into normal text")
			}
		})
	}
}
