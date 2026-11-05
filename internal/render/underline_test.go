package render

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestUnderlineSnapshotAndDeltaRoundTrip(t *testing.T) {
	for _, style := range []string{"4", "4:2", "4:3", "4:4", "4:5"} {
		t.Run(style, func(t *testing.T) {
			source := emu.New(20, 2)
			_ = source.Write([]byte("\x1b[" + style + ";58;2;215;135;95m界é\x1b[24;59mN"))
			snap, _ := source.Snapshot()
			wire, err := proto.Marshal(protocol.SnapshotToProto(snap))
			if err != nil {
				t.Fatal(err)
			}
			restored := new(protocolpb.Snapshot)
			if err := proto.Unmarshal(wire, restored); err != nil {
				t.Fatal(err)
			}
			dest := emu.New(20, 2)
			var out bytes.Buffer
			if err := SnapshotViewportNoClear(&out, restored, 20, 2); err != nil {
				t.Fatal(err)
			}
			_ = dest.Write(out.Bytes())
			got, _ := dest.Snapshot()
			for i, c := range snap.Cells {
				if c != got.Cells[i] {
					t.Fatalf("full cell %d: %+v != %+v", i, c, got.Cells[i])
				}
			}
			next := proto.Clone(restored).(*protocolpb.Snapshot)
			// Only underline color changes; no text/FG/BG/style/cursor change.
			next.UnderlineColor[0] = terminal.ColorIndexed256 | 42
			next.UnderlineColor[1] = terminal.ColorIndexed256 | 42
			out.Reset()
			if err := SnapshotViewportDelta(&out, restored, next, 20, 2); err != nil {
				t.Fatal(err)
			}
			_ = dest.Write(out.Bytes())
			got, _ = dest.Snapshot()
			if got.Cells[0].UnderlineColor != terminal.ColorIndexed256|42 || got.Cells[1].UnderlineColor != terminal.ColorIndexed256|42 {
				t.Fatalf("color-only delta ignored: %+v", got.Cells[:2])
			}
			// An omitted array is the default color, not the previous color.
			cleared := proto.Clone(next).(*protocolpb.Snapshot)
			cleared.UnderlineColor = nil
			out.Reset()
			if err := SnapshotViewportDelta(&out, next, cleared, 20, 2); err != nil {
				t.Fatal(err)
			}
			_ = dest.Write(out.Bytes())
			got, _ = dest.Snapshot()
			if got.Cells[0].UnderlineColor != 0 || got.Cells[0].Mode != snap.Cells[0].Mode {
				t.Fatalf("default color did not restore: %+v", got.Cells[0])
			}
		})
	}
}
