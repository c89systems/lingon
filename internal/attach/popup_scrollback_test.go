package attach

import (
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"testing"
)

func TestPopupScrollbackUsesSourceGridWithoutRenderCache(t *testing.T) {
	c := &Client{TermSize: func() (int, int) { return 100, 9 }, lastSnapshot: mvu.BlankSnapshot(100, 9), scrollbackBuffer: mvu.NewProtoScrollbackBuffer(100)}
	c.scrollbackBuffer.Apply(&protocolpb.Scrollback{Cols: 100, Rows: make([]*protocolpb.ScrollbackRow, 32)})
	// Embedded popup frames are composed by the parent; their own render cache
	// remains empty. Navigation must count the nine live source rows anyway.
	c.SetScrollbackActive(true)
	c.ScrollbackTop(9)
	if got := c.ScrollbackOffset(); got != 32 {
		t.Fatalf("top offset=%d want 32; popup's live rows were omitted", got)
	}
	c.ScrollbackPage(-1, 1)
	if got := c.ScrollbackOffset(); got != 31 {
		t.Fatalf("one row down offset=%d want 31", got)
	}
	c.ScrollbackBottom()
	if got := c.ScrollbackOffset(); got != 0 {
		t.Fatalf("bottom offset=%d", got)
	}
}
