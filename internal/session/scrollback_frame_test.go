package session

import (
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

// This is a storage transaction test: no PTY, command or terminal control exists.
func TestScrollbackFrameCommitsHistoryWithItsLiveGrid(t *testing.T) {
	first := makeSnapshot(20, 3, 0, 2, 0, -1, -1)
	next := makeSnapshot(20, 3, 0, 2, 0, -1, -1)
	local := &localSession{snapshot: first, scrollbackLines: 2}
	local.appendScrollback([]terminal.ScrollbackRow{scrollbackTestRow(20, "one"), scrollbackTestRow(20, "two")})
	local.commitScrollbackFrame(first)
	local.appendScrollback([]terminal.ScrollbackRow{scrollbackTestRow(20, "three")})
	local.snapshot = next
	before := local.scrollbackFrameState()
	if before.live != first || before.end != 2 || string(before.rows[0].Cells[0].Rune) != "o" {
		t.Fatal("uncommitted history/grid became visible")
	}
	local.commitScrollbackFrame(next)
	after := local.scrollbackFrameState()
	if after.live != next || after.end != 3 || after.rows[0].Cells[0].Rune != 't' {
		t.Fatal("committed history/grid did not advance together")
	}
	after.rows[0].Cells[0].Rune = 'x'
	if local.scrollbackFrameState().rows[0].Cells[0].Rune != 't' {
		t.Fatal("reader mutated stored history")
	}
}
