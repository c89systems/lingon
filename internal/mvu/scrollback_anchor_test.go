package mvu

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func historyRows(text string) []*protocolpb.ScrollbackRow {
	rows := make([]*protocolpb.ScrollbackRow, 0, len(text))
	for _, r := range text {
		rows = append(rows, &protocolpb.ScrollbackRow{Runes: []uint32{uint32(r)}})
	}
	return rows
}

func TestScrollbackAnchorRetainsRowsThroughGrowthAndEviction(t *testing.T) {
	b := NewProtoScrollbackBuffer(5)
	b.Apply(&protocolpb.Scrollback{Cols: 4, Rows: historyRows("abcde")})
	var v ScrollbackViewport
	v.EnterAt(7, 2, 3, 4, 4, 0)
	v.SyncHistory(b.Position())
	live := &protocolpb.Snapshot{Cols: 4, Rows: 2, Runes: []uint32{'x', 0, 0, 0, 'y'}}
	first := func() rune {
		t.Helper()
		v.SyncHistory(b.Position())
		v.Normalize(b.Len()+2, 2, 4, 4)
		return rune(BuildScrollbackViewFromProto(4, 2, b.Rows(), live, v.Offset(), v.Column()).Runes[0])
	}
	if got := first(); got != 'c' {
		t.Fatalf("initial anchor %q", got)
	}
	b.Apply(&protocolpb.Scrollback{Cols: 4, Rows: historyRows("fg")})
	if got := first(); got != 'c' {
		t.Fatalf("trimmed buffer moved retained anchor to %q", got)
	}
	v.Page(b.Len()+2, 2, -1, 1)
	if got := first(); got != 'd' {
		t.Fatalf("navigation after growth: %q", got)
	}
	b.Apply(&protocolpb.Scrollback{Cols: 4, Rows: historyRows("h")})
	if got := first(); got != 'd' {
		t.Fatalf("new output moved the new anchor to %q", got)
	}
	b.Apply(&protocolpb.Scrollback{Cols: 4, Rows: historyRows("ijk")})
	if got := first(); got != 'g' || v.Offset() != 5 {
		t.Fatalf("evicted anchor did not clamp to oldest retained row: %q offset=%d", got, v.Offset())
	}
}

func TestScrollbackAnchorEntryBottomAndInactiveHistory(t *testing.T) {
	var v ScrollbackViewport
	v.SyncHistory(ScrollbackPosition{End: 80})
	v.EnterAt(100, 20, 10, 80, 40, 7)
	v.SyncHistory(ScrollbackPosition{End: 80})
	v.SyncHistory(ScrollbackPosition{End: 85})
	v.Normalize(105, 20, 80, 40)
	if v.Offset() != 15 || v.Column() != 7 {
		t.Fatalf("growth moved anchor: %+v", v)
	}
	// Re-rendering the same history must not count growth twice.
	v.SyncHistory(ScrollbackPosition{End: 85})
	if v.Offset() != 15 {
		t.Fatalf("duplicate history advanced the viewport: %+v", v)
	}
	v.Bottom()
	v.SyncHistory(ScrollbackPosition{End: 88})
	if !v.Active() || v.Offset() != 3 {
		t.Fatalf("End began following subsequent output: %+v", v)
	}
	v.Exit()
	v.SyncHistory(ScrollbackPosition{End: 100})
	if v.Active() || v.Offset() != 0 || v.Column() != 0 {
		t.Fatalf("inactive view stopped following output: %+v", v)
	}
	v.EnterAt(120, 20, 0, 80, 40, 0)
	v.SyncHistory(ScrollbackPosition{End: 100})
	if v.Offset() != 0 {
		t.Fatal("re-entry counted output received while inactive")
	}
}

func TestScrollbackAnchorHistoryReplacementAndLimitChange(t *testing.T) {
	b := NewProtoScrollbackBuffer(5)
	b.Apply(&protocolpb.Scrollback{Cols: 4, Rows: historyRows("abcde")})
	initial := b.Position()
	var v ScrollbackViewport
	v.EnterAt(7, 2, 3, 4, 4, 0)
	v.SyncHistory(initial)
	b.Apply(&protocolpb.Scrollback{Cols: 4, Clear: true, Rows: historyRows(strings.Repeat("z", 10))})
	replacement := b.Position()
	if replacement.Generation == initial.Generation || replacement.End != 10 || b.Len() != 5 {
		t.Fatalf("replacement history: %+v rows=%d", replacement, b.Len())
	}
	v.SyncHistory(replacement)
	v.Normalize(b.Len()+2, 2, 4, 4)
	if v.Offset() != 3 {
		t.Fatal("replacement was counted as appended output")
	}
	b.SetLimit(2)
	v.SyncHistory(b.Position())
	v.Normalize(b.Len()+2, 2, 4, 4)
	if v.Offset() != 2 || b.Position() != replacement {
		t.Fatal("retention change lost the position or failed to clamp")
	}
	b.Apply(&protocolpb.Scrollback{Cols: 6, Rows: historyRows("q")})
	if pos := b.Position(); pos.Generation == replacement.Generation || pos.End != 1 || b.Len() != 1 {
		t.Fatalf("width reset retained stale history: %+v rows=%d", pos, b.Len())
	}
	v.SyncHistory(b.Position())
	v.Normalize(b.Len()+2, 2, 6, 4)
	if v.Offset() != 1 {
		t.Fatal("width reset did not clamp")
	}
	b.Apply(&protocolpb.Scrollback{Clear: true})
	v.SyncHistory(b.Position())
	v.Normalize(2, 2, 6, 4)
	if !v.Active() || v.Offset() != 0 {
		t.Fatalf("cleared history left stale viewport bounds: %+v", v)
	}
}

func TestNilScrollbackAnchor(t *testing.T) {
	var v *ScrollbackViewport
	v.SyncHistory(ScrollbackPosition{End: 5})
	var b *ProtoScrollbackBuffer
	if b.Position() != (ScrollbackPosition{}) {
		t.Fatal("nil history has a position")
	}
}

func TestScrollbackAnchorIgnoresStaleHistoryObservations(t *testing.T) {
	var v ScrollbackViewport
	v.Enter()
	v.SyncHistory(ScrollbackPosition{End: 80})
	v.SyncHistory(ScrollbackPosition{End: 85})
	v.SyncHistory(ScrollbackPosition{End: 83})
	v.SyncHistory(ScrollbackPosition{End: 85})
	if v.Offset() != 5 {
		t.Fatalf("stale observation counted output twice: %d", v.Offset())
	}
	v.SyncHistory(ScrollbackPosition{Generation: 1, End: 3})
	v.SyncHistory(ScrollbackPosition{End: 100})
	v.SyncHistory(ScrollbackPosition{Generation: 1, End: 4})
	if v.Offset() != 6 {
		t.Fatalf("stale generation replaced the current anchor: %d", v.Offset())
	}
}
