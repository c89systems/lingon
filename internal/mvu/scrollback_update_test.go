package mvu

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func viewportTestLive(lines ...string) *protocolpb.Snapshot {
	s := BlankSnapshot(24, len(lines))
	for y, line := range lines {
		for x, r := range []rune(line) {
			s.Runes[y*24+x] = uint32(r)
		}
	}
	s.Cursor = &protocolpb.Cursor{Y: uint32(len(lines) - 1)}
	return s
}
func viewportTestLine(s *protocolpb.Snapshot, row int) string {
	line := make([]rune, s.Cols)
	for x := range line {
		line[x] = rune(s.Runes[row*int(s.Cols)+x])
	}
	return string(line)
}

func TestScrollbackUpdateBeforeViewAndPendingLiveCoordinates(t *testing.T) {
	live := viewportTestLive("live-a", "live-b", "live-c", "live-d")
	data := ScrollbackContent{Live: live, History: NewProtoScrollbackSource(historyRows("12345")), Position: ScrollbackPosition{End: 5}, LivePosition: ScrollbackPosition{End: 5}, Cols: 24, Rows: 4}
	var model ScrollbackViewport
	model.Update(ScrollbackUpdate{Content: &data, Operation: ScrollbackEnter})
	before := model.View()
	history := historyRows("12345")
	history = append(history, &protocolpb.ScrollbackRow{Runes: append([]uint32(nil), live.Runes[:24]...)})
	data.History = NewProtoScrollbackSource(history)
	data.Position.End++
	model.Update(ScrollbackUpdate{Content: &data})
	if model.Offset() != 1 {
		t.Fatal("Update failed to anchor before View")
	}
	pending := model.View()
	if !proto.Equal(before.Snapshot, pending.Snapshot) {
		t.Fatalf("history-only input exposed a torn live grid: before=%q after=%q", viewportTestLine(before.Snapshot, 1), viewportTestLine(pending.Snapshot, 1))
	}
	// The remaining visible live rows still update in place when the live frame arrives.
	data.Live = viewportTestLive("live-b", "live-c", "edited-live-d", "new-bottom")
	data.LivePosition = data.Position
	model.Update(ScrollbackUpdate{Content: &data})
	changed := model.View()
	if viewportTestLine(changed.Snapshot, 0) != viewportTestLine(before.Snapshot, 0) || viewportTestLine(changed.Snapshot, 1) != viewportTestLine(before.Snapshot, 1) {
		t.Fatal("live edit moved the viewport")
	}
	if got := viewportTestLine(changed.Snapshot, 3); got[:13] != "edited-live-d" {
		t.Fatalf("live edit was frozen: %q", got)
	}
	snapshot := proto.Clone(data.Live)
	state := model
	one, two := model.View(), model.View()
	if !reflect.DeepEqual(model, state) || !proto.Equal(one.Snapshot, two.Snapshot) || !proto.Equal(snapshot, data.Live) {
		t.Fatal("View mutated model or input data")
	}
}

func TestScrollbackUpdateSourceAdaptersAndNavigation(t *testing.T) {
	rows := []terminal.ScrollbackRow{}
	for _, r := range "abcdef" {
		rows = append(rows, terminal.ScrollbackRow{Cols: 24, Cells: []terminal.Cell{{Rune: r, FG: terminal.ColorDefault, BG: terminal.ColorDefault}}})
	}
	for _, source := range []ScrollbackSource{NewTerminalScrollbackSource(rows), NewProtoScrollbackSource(historyRows("abcdef"))} {
		var model ScrollbackViewport
		data := ScrollbackContent{Live: viewportTestLive("live-a", "live-b"), History: source, Position: ScrollbackPosition{End: 6}, LivePosition: ScrollbackPosition{End: 6}, Cols: 24, Rows: 2}
		model.Update(ScrollbackUpdate{Content: &data, Operation: ScrollbackEnter})
		model.Update(ScrollbackUpdate{Operation: ScrollbackTop})
		if got := model.View().Snapshot.Runes[0]; got != 'a' {
			t.Fatalf("top projected %q", rune(got))
		}
		model.Update(ScrollbackUpdate{Operation: ScrollbackPage, Delta: -1, Step: 1})
		if got := model.View().Snapshot.Runes[0]; got != 'b' {
			t.Fatalf("page projected %q", rune(got))
		}
		model.Update(ScrollbackUpdate{Operation: ScrollbackBottom})
		if !model.Active() || model.Offset() != 0 {
			t.Fatal("bottom exited mode")
		}
		model.Update(ScrollbackUpdate{Operation: ScrollbackExit})
		if model.View().Visible || model.View().Snapshot != data.Live {
			t.Fatal("exit did not resume live model")
		}
	}
}

func TestPendingLiveProjectionHandlesSparseAttributes(t *testing.T) {
	live := &protocolpb.Snapshot{Cols: 24, Rows: 4, Runes: []uint32{'x'}, Modes: []int32{1}, Graphemes: []string{"x"}}
	out := alignPendingLiveRows(live, 1)
	if !proto.Equal(live, out) {
		t.Fatal("sparse input was altered")
	}
}

func TestScrollbackUpdateSeparatesSessionCoordinates(t *testing.T) {
	var model ScrollbackViewport
	older := ScrollbackContent{SessionID: "old", Live: viewportTestLive("old"), Position: ScrollbackPosition{Generation: 9, End: 500}}
	model.Update(ScrollbackUpdate{Content: &older})
	newer := ScrollbackContent{SessionID: "new", Live: viewportTestLive("new")}
	model.Update(ScrollbackUpdate{Content: &newer})
	if model.View().Snapshot != newer.Live {
		t.Fatal("another session's history position suppressed the selected session")
	}
	model.Update(ScrollbackUpdate{Content: &older, Operation: ScrollbackEnter})
	model.Update(ScrollbackUpdate{Content: &newer})
	if model.Active() || model.View().Snapshot != newer.Live {
		t.Fatal("scrollback state leaked across session identity")
	}
}
