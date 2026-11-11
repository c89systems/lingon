package attach

import (
	"pkt.systems/lingon/internal/protocolpb"
	"testing"
)

func TestOSC8DiffClearsOldTargetsAndHistoryShiftPreservesThem(t *testing.T) {
	c := &Client{}
	first := &protocolpb.Diff{Cols: 4, Rows: 2, DiffRows: []*protocolpb.DiffRow{{Row: 0, Runes: []uint32{'l', 'i', 'n', 'k'}, Hyperlinks: []string{";https://example.invalid/", ";https://example.invalid/", ";https://example.invalid/", ";https://example.invalid/"}}}}
	snap := c.applyDiff(first)
	if len(snap.Hyperlinks) != 8 || snap.Hyperlinks[0] == "" {
		t.Fatal("diff omitted hyperlink allocation")
	}
	saved := cloneSnapshot(snap)
	shifted := shiftScrollbackLive(snap, 1)
	if shifted.Hyperlinks[0] != "" || saved.Hyperlinks[0] == "" {
		t.Fatal("history shift retained wrong row or mutated original")
	}
	snap = c.applyDiff(&protocolpb.Diff{Cols: 4, Rows: 2, DiffRows: []*protocolpb.DiffRow{{Row: 0, Runes: []uint32{'l', 'i', 'n', 'k'}}}})
	for _, link := range snap.Hyperlinks {
		if link != "" {
			t.Fatal("same-text plain diff retained a stale target")
		}
	}
	if saved.Hyperlinks[0] == "" {
		t.Fatal("applying diff mutated snapshot copy")
	}
}
