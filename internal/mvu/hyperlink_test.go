package mvu

import (
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"testing"
)

func TestOSC8HyperlinksSurviveHistoryCompositionAndOverlays(t *testing.T) {
	e := emu.New(20, 4)
	_ = e.Write([]byte("\x1b]8;id=history;https://example.invalid/history\aLINK\x1b]8;;\a\r\n"))
	snapshot, _ := e.Snapshot()
	cells := append([]terminal.Cell(nil), snapshot.Cells[:20]...)
	history := []terminal.ScrollbackRow{{Cols: 20, Cells: cells}}
	live := BlankSnapshot(20, 4)
	terminalView := BuildScrollbackViewFromTerminal(20, 4, history, live, 1, 0)
	protocolView := BuildScrollbackViewFromProto(20, 4, []*protocolpb.ScrollbackRow{protocol.ScrollbackRowToProto(history[0])}, live, 1, 0)
	if len(terminalView.Hyperlinks) == 0 || terminalView.Hyperlinks[0] != cells[0].Hyperlink || !proto.Equal(terminalView, protocolView) {
		t.Fatal("history link missing or transport-dependent")
	}
	frame, err := ComposeViewportSnapshot(terminalView, 20, 4, Resolved{}, Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Hyperlinks) == 0 || frame.Hyperlinks[0] != cells[0].Hyperlink {
		t.Fatal("composed framebuffer lost link")
	}
	// Replacing link cells with plain popup/header cells must erase old targets.
	overlay := BlankSnapshot(20, 1)
	copyPopupCells(frame, 0, overlay)
	for _, link := range frame.Hyperlinks[:20] {
		if link != "" {
			t.Fatal("plain overlay retained underlying hyperlink")
		}
	}
	clipped := BuildScrollbackViewFromTerminal(10, 4, history, live, 1, 2)
	if len(clipped.Hyperlinks) == 0 || clipped.Hyperlinks[0] != cells[2].Hyperlink {
		t.Fatal("horizontal crop lost link")
	}
}
