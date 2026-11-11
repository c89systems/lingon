package relaywire

import (
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"testing"
)

func TestDiffSnapshotsUnderlineOnlyChanges(t *testing.T) {
	base := snapshotForTest(2, 1, []rune{'a', 'b'})
	next := proto.Clone(base).(*protocolpb.Snapshot)
	next.UnderlineColor = []uint32{terminal.ColorTrue | 0xd7875f, 0}
	diff, full := DiffSnapshots(base, next)
	if full || diff == nil || len(diff.DiffRows) != 1 || diff.DiffRows[0].UnderlineColor[0] != next.UnderlineColor[0] {
		t.Fatalf("underline-only change lost: %+v", diff)
	}
	diff, full = DiffSnapshots(next, base)
	if full || diff == nil || len(diff.DiffRows) != 1 || len(diff.DiffRows[0].UnderlineColor) != 0 {
		t.Fatalf("default underline reset lost: %+v", diff)
	}
	same := proto.Clone(base).(*protocolpb.Snapshot)
	same.UnderlineColor = make([]uint32, 2)
	if diff, full = DiffSnapshots(base, same); full || diff != nil {
		t.Fatal("absent vs default colors caused a redundant frame")
	}
}
