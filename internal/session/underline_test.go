package session

import (
	"pkt.systems/lingon/internal/terminal"
	"testing"
)

func TestUnderlineColorSurvivesSnapshotCopies(t *testing.T) {
	src := blankSnapshot(4, 3)
	src.Runes[5] = 'X'
	src.Modes[5] = int32(terminal.ModeUnderlineCurly)
	src.UnderlineColor[5] = terminal.ColorTrue | 0xd7875f
	cloned := cloneSnapshot(src)
	cloned.UnderlineColor[5] = terminal.ColorIndexed256 | 42
	if src.UnderlineColor[5] != terminal.ColorTrue|0xd7875f {
		t.Fatal("clone aliases underline color")
	}
	dst := blankSnapshot(2, 2)
	copySnapshotRegionFromOffset(dst, src, 1, 1, 2, 2)
	if dst.Runes[0] != 'X' || dst.UnderlineColor[0] != src.UnderlineColor[5] {
		t.Fatal("crop lost underline color")
	}
	dst = blankSnapshot(6, 5)
	copySnapshotRegionAtOffset(dst, src, 1, 1, 4, 3)
	if dst.Runes[14] != 'X' || dst.UnderlineColor[14] != src.UnderlineColor[5] {
		t.Fatal("offset copy lost underline color")
	}
	rows := snapshotToScrollbackRows(src)
	if rows[1].Cells[1].UnderlineColor != src.UnderlineColor[5] {
		t.Fatal("scrollback conversion lost underline color")
	}
	src.UnderlineColor = nil
	copySnapshotRegionAtOffset(dst, src, 1, 1, 4, 3)
	if dst.UnderlineColor[14] != 0 {
		t.Fatal("copy retained stale underline color")
	}
}
