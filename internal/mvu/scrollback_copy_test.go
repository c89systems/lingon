package mvu

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func copyViewport() (ScrollbackViewport, ScrollbackSource) {
	source := ScrollbackSource{Position: ScrollbackPosition{End: 20}, HistoryRows: 20, LiveRows: 10, ViewRows: 10}
	var v ScrollbackViewport
	v.EnterAt(30, 10, 10, 120, 80, 12)
	v.SyncSource(source)
	v.ToggleVisual()
	return v, source
}

func TestScrollbackVisualCursorNavigationAndSelection(t *testing.T) {
	v, source := copyViewport()
	if !v.Visual() || v.copy.cursor != 14 || v.Column() != 0 {
		t.Fatalf("entry: %+v", v)
	}
	if v.PanX(120, 80, 1) || v.Column() != 0 {
		t.Fatal("visual cursor moved horizontally")
	}
	v.Page(30, 10, 1, 1)
	if v.copy.cursor != 13 || v.Offset() != 10 {
		t.Fatalf("cursor movement scrolled early: %+v", v)
	}
	v.Page(30, 10, 1, 5)
	if v.copy.cursor != 8 || v.Offset() != 12 {
		t.Fatalf("edge did not follow cursor: %+v", v)
	}
	v.Top(30, 10)
	if v.copy.cursor != 0 || v.Offset() != 20 {
		t.Fatalf("top: %+v", v)
	}
	if _, copied := v.SelectLines(); copied {
		t.Fatal("first space copied")
	}
	v.Page(30, 10, -1, 5)
	before := v
	base := BlankSnapshot(80, 10)
	decorated := v.DecorateCopy(base, source)
	if v != before || !proto.Equal(base, BlankSnapshot(80, 10)) {
		t.Fatal("copy projection mutated state or base")
	}
	for row := 0; row < 10; row++ {
		for col := 0; col < 80; col++ {
			if got := decorated.Modes[row*80+col]&int32(terminal.ModeInverse) != 0; got != (row <= 5) {
				t.Fatalf("selection row %d col %d: %v", row, col, got)
			}
		}
	}
	if decorated.Cursor.GetX() != 0 || decorated.Cursor.GetY() != 5 {
		t.Fatal(decorated.Cursor)
	}
	lines, copied := v.SelectLines()
	if !copied || lines != (ScrollbackLineRange{First: 0, Last: 5}) || !v.Visual() || v.copy.selecting {
		t.Fatalf("copy: %+v %+v", lines, v)
	}
	if v.DecorateCopy(base, source).Modes[0] != 0 {
		t.Fatal("copied selection stayed inverted")
	}
	v.Bottom()
	if v.copy.cursor != 29 || v.Offset() != 0 {
		t.Fatalf("bottom: %+v", v)
	}
	v.Page(30, 10, -1, 1000)
	if v.copy.cursor != 29 {
		t.Fatal("cursor moved past bottom")
	}
	v.ToggleVisual()
	if v.Visual() {
		t.Fatal("v did not leave visual mode")
	}
	if !v.PanX(120, 80, 1) {
		t.Fatal("ordinary horizontal panning was disabled")
	}
}

func TestScrollbackVisualStaysAnchoredThroughOutputResizeAndEviction(t *testing.T) {
	v, source := copyViewport()
	v.SelectLines()
	source.Position.End = 25
	v.SyncSource(source)
	if v.copy.cursor != 14 || v.copy.anchor != 14 || v.Offset() != 15 {
		t.Fatalf("output moved selected content: %+v", v)
	}
	frame := v.DecorateCopy(BlankSnapshot(80, 10), source)
	if frame.Cursor.GetY() != 4 {
		t.Fatalf("output moved cursor on screen: %v", frame.Cursor)
	}
	source.ViewRows = 3
	v.SyncSource(source)
	frame = v.DecorateCopy(BlankSnapshot(80, 3), source)
	if frame.Cursor.GetY() != 2 {
		t.Fatalf("resize lost cursor: %v", frame.Cursor)
	}
	source.Position.End = 40
	v.SyncSource(source)
	if v.copy.cursor != 20 || v.copy.anchor != 20 {
		t.Fatalf("eviction did not clamp to retained rows: %+v", v)
	}
	source.Position = ScrollbackPosition{Generation: 1, End: 2}
	source.HistoryRows = 2
	v.SyncSource(source)
	if v.Visual() || v.copy.selecting {
		t.Fatal("replacement retained a stale selection")
	}
	for _, exit := range []func(){v.Exit, func() { v.SetActive(false) }} {
		v.EnterAt(12, 3, 0, 80, 80, 0)
		v.SyncSource(source)
		v.ToggleVisual()
		v.SelectLines()
		exit()
		if v.Visual() || v.copy.selecting {
			t.Fatal("exit retained visual selection")
		}
	}
}

func TestCopyScrollbackFullRowsUnicodeAndBlankLines(t *testing.T) {
	live := BlankSnapshot(12, 1)
	live.Runes[0] = '界'
	live.Runes[2] = 'x'
	live.Runes[3] = 'e'
	live.Runes[4] = 0x1b
	live.Graphemes = make([]string, 12)
	live.Graphemes[3] = "e\u0301"
	terminalRows := []terminal.ScrollbackRow{{Cells: []terminal.Cell{{Rune: 'a'}, {Rune: ' '}, {Rune: 'b'}, {Rune: ' '}}}, {Cells: []terminal.Cell{{Rune: ' '}}}}
	protoRows := []*protocolpb.ScrollbackRow{{Runes: []uint32{'a', ' ', 'b', ' '}}, {Runes: []uint32{' '}}}
	for _, source := range []ScrollbackTextSource{NewTerminalTextSource(terminalRows, live), NewProtoTextSource(protoRows, live)} {
		if got := CopyScrollbackLines(source, ScrollbackLineRange{First: 0, Last: 2}); got != "a b\n\n界xe\u0301\n" {
			t.Fatalf("copied text: %q", got)
		}
		if got := CopyScrollbackLines(source, ScrollbackLineRange{First: 1, Last: 1}); got != "\n" {
			t.Fatalf("blank line: %q", got)
		}
	}
}
