package emu

import (
	"pkt.systems/lingon/internal/terminal"
	"testing"
)

func TestOSC8HyperlinksAreIndependentOfSGRAndChunking(t *testing.T) {
	for _, end := range []string{"\x1b\\", "\a", "\x9c"} {
		e := New(30, 3)
		input := "\x1b]8;id=x:foo=bar;https://example.invalid/a;b" + end + "a\x1b[0mb界\x1b]8;;" + end + "\x1b[4mz"
		for _, b := range []byte(input) {
			if err := e.Write([]byte{b}); err != nil {
				t.Fatal(err)
			}
		}
		snap, err := e.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for x := 0; x < 4; x++ {
			if snap.Cells[x].Hyperlink != "id=x:foo=bar;https://example.invalid/a;b" {
				t.Fatalf("linked cell %d: %+v", x, snap.Cells[x])
			}
		}
		if snap.Cells[4].Hyperlink != "" || snap.Cells[4].Mode&terminal.ModeUnderline == 0 {
			t.Fatal("ordinary underline became a link")
		}
		_ = e.Write([]byte("\x1b[H\x1b[2K"))
		snap, _ = e.Snapshot()
		if snap.Cells[0].Hyperlink != "" {
			t.Fatal("erase retained a link")
		}
		_ = e.Write([]byte("\x1b]8;;https://example.invalid/\a\x1bcX"))
		snap, _ = e.Snapshot()
		if snap.Cells[0].Hyperlink != "" {
			t.Fatal("reset retained active hyperlink")
		}
	}
}

func TestOSC8UTF8URIIsNotMistakenForC1Terminator(t *testing.T) {
	e := New(20, 2)
	_ = e.Write([]byte("\x1b]8;;https://example.invalid/Ü\x1b\\X\x1b]8;;\x1b\\"))
	snap, _ := e.Snapshot()
	if snap.Cells[0].Hyperlink != ";https://example.invalid/Ü" {
		t.Fatalf("UTF-8 URI corrupted: %+v", snap.Cells[0])
	}
}
