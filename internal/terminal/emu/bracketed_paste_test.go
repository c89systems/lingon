package emu

import (
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestBracketedPasteModeSnapshotRestoreAndReset(t *testing.T) {
	e := New(80, 20)
	for _, data := range []string{"\x1b[?20", "04h"} {
		if err := e.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Mode&terminal.SnapshotModeBracketedPaste == 0 {
		t.Fatal("mode 2004 was not captured")
	}
	other := New(80, 20)
	other.LoadSnapshot(snap)
	got, _ := other.Snapshot()
	if got.Mode&terminal.SnapshotModeBracketedPaste == 0 {
		t.Fatal("snapshot restore lost mode")
	}
	for _, sequence := range []string{"\x1b[?2004l", "\x1b[?2004h\x1bc"} {
		if err := e.Write([]byte(sequence)); err != nil {
			t.Fatal(err)
		}
		got, _ = e.Snapshot()
		if got.Mode&terminal.SnapshotModeBracketedPaste != 0 {
			t.Fatalf("mode remained after %q", sequence)
		}
	}
}
