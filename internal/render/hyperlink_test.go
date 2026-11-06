package render

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestOSC8HyperlinkSnapshotAndDeltaRoundTrip(t *testing.T) {
	e := emu.New(20, 3)
	_ = e.Write([]byte("\x1b]8;id=x;https://example.invalid/old\x1b\\link\x1b]8;;\x1b\\\x1b[4m plain\x1b[0m"))
	screen, _ := e.Snapshot()
	previous := protocol.SnapshotToProto(screen)
	for _, dim := range []bool{false, true} {
		var out bytes.Buffer
		var err error
		if dim {
			err = SnapshotViewportDim(&out, previous, 20, 3)
		} else {
			err = Snapshot(&out, previous)
		}
		if err != nil {
			t.Fatal(err)
		}
		receiver := emu.New(20, 3)
		_ = receiver.Write(out.Bytes())
		_ = receiver.Write([]byte("\x1b[3;1Houtside"))
		got, _ := receiver.Snapshot()
		if got.Cells[0].Hyperlink != screen.Cells[0].Hyperlink || got.Cells[5].Hyperlink != "" || got.Cells[40].Hyperlink != "" {
			t.Fatal("link target lost or leaked beyond its span/frame")
		}
	}
	_ = e.Write([]byte("\x1b[H\x1b]8;id=y;https://example.invalid/new\a" + "link" + "\x1b]8;;\a"))
	screen, _ = e.Snapshot()
	next := protocol.SnapshotToProto(screen)
	diff, full := relaywire.DiffSnapshots(previous, next)
	if full || diff == nil || len(diff.DiffRows) != 1 {
		t.Fatal("link-only change did not produce a row diff")
	}
	var out bytes.Buffer
	if err := SnapshotViewportDelta(&out, previous, next, 20, 3); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "id=y;https://example.invalid/new") {
		t.Fatal("same-text link retarget missing")
	}
	receiver := emu.New(20, 3)
	receiver.LoadSnapshot(protocol.SnapshotFromProto(previous))
	_ = receiver.Write(out.Bytes())
	got, _ := receiver.Snapshot()
	if got.Cells[0].Hyperlink != screen.Cells[0].Hyperlink {
		t.Fatal("retarget did not update the displayed link")
	}
	bad := protocol.SnapshotToProto(screen)
	bad.Hyperlinks[0] = ";https://example.invalid/\x1b[2J"
	out.Reset()
	_ = Snapshot(&out, bad)
	if strings.Contains(out.String(), "https://example.invalid/\x1b[2J") {
		t.Fatal("untrusted hyperlink injected terminal controls")
	}
}

func TestOSC8FullWidthRowsCloseBeforeFollowingOutput(t *testing.T) {
	e := emu.New(4, 2)
	_ = e.Write([]byte("\x1b]8;;https://example.invalid/\aabcd"))
	screen, _ := e.Snapshot()
	snap := protocol.SnapshotToProto(screen)
	for _, mode := range []string{"full", "delta", "dim"} {
		t.Run(mode, func(t *testing.T) {
			var out bytes.Buffer
			switch mode {
			case "full":
				_ = Snapshot(&out, snap)
			case "delta":
				blank := emu.New(4, 2)
				base, _ := blank.Snapshot()
				_ = SnapshotViewportDelta(&out, protocol.SnapshotToProto(base), snap, 4, 2)
			case "dim":
				_ = SnapshotViewportDim(&out, snap, 4, 2)
			}
			receiver := emu.New(4, 2)
			_ = receiver.Write(out.Bytes())
			_ = receiver.Write([]byte("\x1b[2;1Hx"))
			got, _ := receiver.Snapshot()
			if got.Cells[0].Hyperlink == "" || got.Cells[4].Hyperlink != "" {
				t.Fatal("full-width link lost or leaked beyond its frame")
			}
		})
	}
}

func TestOSC8DimRetainsLinkedSpacesAndClosesBeforeErase(t *testing.T) {
	e := emu.New(8, 2)
	_ = e.Write([]byte("\x1b]8;;https://example.invalid/\aX \x1b]8;;\a"))
	source, _ := e.Snapshot()
	var out bytes.Buffer
	_ = SnapshotViewportDim(&out, protocol.SnapshotToProto(source), 8, 2)
	raw := out.String()
	closeAt := strings.Index(raw, "\x1b]8;;\x1b\\")
	eraseAt := strings.Index(raw, "\x1b[K")
	if closeAt < 0 || closeAt > eraseAt {
		t.Fatal("dimmed row erased its tail with a hyperlink still active")
	}
	receiver := emu.New(8, 2)
	_ = receiver.Write(out.Bytes())
	got, _ := receiver.Snapshot()
	if got.Cells[1].Hyperlink != source.Cells[1].Hyperlink {
		t.Fatal("dimmed row discarded a linked space")
	}
}
