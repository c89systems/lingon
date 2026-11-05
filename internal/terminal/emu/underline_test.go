package emu

import (
	"fmt"
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestUnderlineStylesAndResets(t *testing.T) {
	for _, tc := range []struct {
		sgr  string
		mode int16
	}{
		{"4", terminal.ModeUnderline}, {"4:1", terminal.ModeUnderline},
		{"4:2", terminal.ModeUnderlineDouble}, {"21", terminal.ModeUnderlineDouble},
		{"4:3", terminal.ModeUnderlineCurly}, {"4:4", terminal.ModeUnderlineDotted},
		{"4:5", terminal.ModeUnderlineDashed}, {"24", 0}, {"4:0", 0}, {"0", 0},
		{"4:", terminal.ModeUnderline},
	} {
		t.Run(tc.sgr, func(t *testing.T) {
			e := New(4, 1)
			_ = e.Write([]byte("\x1b[4:3;58;5;42mA\x1b[" + tc.sgr + "mB\x1b[4mC\x1b[24mD"))
			snap, _ := e.Snapshot()
			if snap.Cells[1].Mode != tc.mode || snap.Cells[2].Mode != terminal.ModeUnderline || snap.Cells[3].Mode != 0 {
				t.Fatalf("styles/reset: %+v", snap.Cells)
			}
			want := terminal.ColorIndexed256 | 42
			if tc.sgr == "0" {
				want = 0
			}
			if snap.Cells[1].UnderlineColor != want {
				t.Fatalf("style reset changed independent color: %+v", snap.Cells[1])
			}
		})
	}
}

func TestUnderlineColorFormsDoNotAlterOtherAttributes(t *testing.T) {
	for _, tc := range []struct {
		sgr   string
		color uint32
	}{
		{"58;2;215;135;95", terminal.ColorTrue | 0xd7875f},
		{"58:2:215:135:95", terminal.ColorTrue | 0xd7875f},
		{"58:2::215:135:95", terminal.ColorTrue | 0xd7875f},
		{"58:2:0:215:135:95", terminal.ColorTrue | 0xd7875f},
		{"58;2;0;5;7", terminal.ColorTrue | 0x000507},
		{"58:2::0:5:7", terminal.ColorTrue | 0x000507},
		{"58;5;0", terminal.ColorIndexed256}, {"58:5:123", terminal.ColorIndexed256 | 123},
		{"59", 0}, {"58:2:7:1:2:3", 0}, {"58:2::999:0:1", 0},
		{"58;2;999;0;1", 0}, {"58:5:999", 0}, {"58;2;1", 0},
		{"99:0:2:7", 0}, {"58:2::1:2:3:4", 0},
	} {
		t.Run(tc.sgr, func(t *testing.T) {
			e := New(1, 1)
			input := fmt.Sprintf("\x1b[1;3;4:3;38;2;224;224;224;48;2;38;34;48m\x1b[%smA", tc.sgr)
			// Split every control sequence across writes as a PTY may do.
			for _, b := range []byte(input) {
				_ = e.Write([]byte{b})
			}
			snap, _ := e.Snapshot()
			cell := snap.Cells[0]
			if cell.Mode != terminal.ModeBold|terminal.ModeItalic|terminal.ModeUnderlineCurly || cell.FG != terminal.ColorTrue|0xe0e0e0 || cell.BG != terminal.ColorTrue|0x262230 || cell.UnderlineColor != tc.color {
				t.Fatalf("color contaminated attributes: %+v", cell)
			}
		})
	}
}

func TestUnderlineColorSaveRestoreAndWideCells(t *testing.T) {
	e := New(8, 2)
	_ = e.Write([]byte("\x1b[4:3;58;2;215;135;95m界\x1b7\x1b[0mA\x1b8B\x1b[?1049h\x1b[0m\x1b[?1049lC"))
	snap, _ := e.Snapshot()
	for _, x := range []int{0, 1, 2, 3} {
		if snap.Cells[x].Mode != terminal.ModeUnderlineCurly || snap.Cells[x].UnderlineColor != terminal.ColorTrue|0xd7875f {
			t.Fatalf("saved/wide attributes at %d: %+v", x, snap.Cells[x])
		}
	}
}
