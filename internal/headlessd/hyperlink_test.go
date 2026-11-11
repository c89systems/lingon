package headlessd

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestCloneScrollbackRetainsHyperlinks(t *testing.T) {
	row := &protocolpb.ScrollbackRow{Runes: []uint32{'x'}, Hyperlinks: []string{"id=a;https://example.test"}}
	clone := cloneScrollbackRow(row)
	if len(clone.Hyperlinks) != 1 || clone.Hyperlinks[0] != row.Hyperlinks[0] {
		t.Fatal("scrollback clone discarded OSC 8 target")
	}
	clone.Hyperlinks[0] = ""
	if row.Hyperlinks[0] == "" {
		t.Fatal("scrollback clone aliases original targets")
	}
}
