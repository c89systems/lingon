package attach

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"testing"
)

func TestUnderlineColorDiffReplacesAndClears(t *testing.T) {
	c := &Client{lastSnapshot: &protocolpb.Snapshot{Cols: 2, Rows: 1, Runes: []uint32{'a', 'b'}, Modes: []int32{8, 8}, Fg: make([]uint32, 2), Bg: make([]uint32, 2)}}
	color := uint32(terminal.ColorTrue | 0xd7875f)
	diff := &protocolpb.Diff{Cols: 2, Rows: 1, DiffRows: []*protocolpb.DiffRow{{Row: 0, UnderlineColor: []uint32{color, color}}}}
	if got := c.applyDiff(diff); got.UnderlineColor[0] != color || got.UnderlineColor[1] != color {
		t.Fatal("diff lost underline color")
	}
	diff.DiffRows[0].UnderlineColor = nil
	if got := c.applyDiff(diff); got.UnderlineColor[0] != 0 || got.UnderlineColor[1] != 0 || got.Modes[0] != 8 {
		t.Fatal("omitted default color retained stale underline color")
	}
}
