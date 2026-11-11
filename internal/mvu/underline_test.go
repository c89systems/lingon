package mvu

import (
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/theme"
	"testing"
)

func TestUnderlineColorsSurviveScrollbackAndPopupComposition(t *testing.T) {
	color := uint32(terminal.ColorTrue | 0xd7875f)
	cell := terminal.Cell{Rune: 'X', Mode: terminal.ModeUnderlineCurly, FG: terminal.ColorTrue | 0xe0e0e0, BG: terminal.ColorTrue | 0x262230, UnderlineColor: color}
	row := terminal.ScrollbackRow{Cols: 2, Cells: []terminal.Cell{cell, {Rune: ' ', Mode: terminal.ModeUnderline, UnderlineColor: color}}}
	live := protocol.SnapshotToProto(terminal.Snapshot{Cols: 2, Rows: 1, Cells: row.Cells})
	buf := NewProtoScrollbackBuffer(10)
	buf.Apply(&protocolpb.Scrollback{Cols: 2, Rows: []*protocolpb.ScrollbackRow{protocol.ScrollbackRowToProto(row)}})
	for _, view := range []*protocolpb.Snapshot{
		BuildScrollbackViewFromTerminal(2, 2, []terminal.ScrollbackRow{row}, live, 0, 0),
		BuildScrollbackViewFromProto(2, 2, buf.Rows(), live, 0, 0),
	} {
		for _, i := range []int{0, 2} {
			if view.UnderlineColor[i] != color || view.Modes[i] != int32(terminal.ModeUnderlineCurly) || view.Bg[i] != cell.BG {
				t.Fatalf("scrollback/live attributes lost: %+v", view)
			}
		}
		if view.UnderlineColor[1] != 0 || view.UnderlineColor[3] != 0 {
			t.Fatal("trailing blank attributes retained")
		}
	}
	// Compose a real popup framebuffer: terminal attributes must survive both
	// emulation passes and the pane copy, without decorating the separator.
	base := BlankSnapshot(20, 12)
	pane := BlankSnapshot(20, PopupRows(12))
	pane.Runes[0] = 'X'
	pane.Modes[0] = int32(cell.Mode)
	pane.UnderlineColor[0] = color
	frame, err := composePopup(base, 20, 12, State{Theme: theme.TUI("default"), Popup: &Popup{Name: "tools", Snapshot: pane}})
	if err != nil {
		t.Fatal(err)
	}
	first := (12 - PopupRows(12)) * 20
	if frame.Runes[first] != 'X' || frame.UnderlineColor[first] != color || frame.Modes[first] != int32(cell.Mode) {
		t.Fatalf("popup attributes lost: %+v", frame)
	}
	if frame.UnderlineColor[first-20] != 0 {
		t.Fatal("pane color leaked into separator")
	}
}
