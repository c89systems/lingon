package mvu

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/lingon/internal/theme"
)

func TestImportantTabIdentityAndImmutableSnapshots(t *testing.T) {
	rt := NewRuntime()
	if result := rt.ApplyAction(ImportantTabAction{}); result.Changed || rt.Read().ImportantTabs.Active() {
		t.Fatal("empty identity entered important mode")
	}
	if result := rt.ApplyAction(ImportantTabAction{SessionID: "a"}); !result.Changed || result.ForceFull {
		t.Fatal("mark did not change the model, or requested a full redraw")
	}
	first := rt.Read()
	if rt.ApplyAction(ImportantTabAction{SessionID: "a"}).Changed {
		t.Fatal("marking twice toggled importance")
	}
	rt.ApplyAction(ImportantTabAction{SessionID: "b"})
	if first.ImportantTabs.Contains("b") || !first.ImportantTabs.Contains("a") {
		t.Fatal("a later mark mutated an earlier frame snapshot")
	}
	rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "a", Name: "renamed"}, {ID: "new-b", Name: "b"}}, ActiveID: "new-b"}})
	rt.ApplyAction(ContextAction{Input: ContextInput{Theme: theme.TUI("doom-nord")}})
	state := rt.Read()
	if !state.ImportantTabs.Contains("a") || state.ImportantTabs.Contains("new-b") || !state.ImportantTabs.Active() {
		t.Fatal("rename, removal, reused name, or theme update changed identity-based marks")
	}
	rt.ApplyAction(ImportantTabAction{Reset: true})
	if rt.Read().ImportantTabs.Active() || !state.ImportantTabs.Contains("b") {
		t.Fatal("reset left marks or mutated a previous snapshot")
	}
	if NewRuntime().Read().ImportantTabs.Active() {
		t.Fatal("marks leaked into a different UI runtime")
	}
}

func importantTabScreen(t *testing.T, state State) terminal.Snapshot {
	t.Helper()
	data := ComposeResolved(nil, 80, 8, Cursor{Row: 3, Col: 1, Visible: true}, Resolve(state, Cursor{Row: 3}, time.Time{}, ResolveOptions{}))
	e := emu.New(80, 8)
	if err := e.Write(data); err != nil {
		t.Fatal(err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestImportantTabColorsAndSearchComposition(t *testing.T) {
	for _, name := range theme.Names() {
		t.Run(name, func(t *testing.T) {
			rt := NewRuntime()
			rt.ApplyAction(ContextAction{Input: ContextInput{Theme: theme.TUI(name)}})
			rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "a", Name: "one"}, {ID: "b", Name: "two"}, {ID: "c", Name: "three"}}, ActiveID: "a"}})
			baseline := importantTabScreen(t, rt.Read())
			rt.ApplyAction(ImportantTabAction{SessionID: "a"})
			marked := importantTabScreen(t, rt.Read())
			if marked.Cells[1] != baseline.Cells[1] {
				t.Fatal("marked tab lost original active theme")
			}
			if marked.Cells[6].BG == baseline.Cells[6].BG || marked.Cells[79].BG == baseline.Cells[79].BG {
				t.Fatal("unmarked tab or trailing bar did not dim")
			}
			rt.ApplyAction(ImportantTabAction{SessionID: "b"})
			both := importantTabScreen(t, rt.Read())
			if both.Cells[6] != baseline.Cells[6] || both.Cells[79].BG != marked.Cells[79].BG {
				t.Fatal("second mark did not restore just its original theme")
			}
			state := rt.Read()
			state.ActiveTab = 2
			state.Search = SearchState{Visible: true, Text: "th", Ghost: "ree", CandidateID: "c"}
			search := importantTabScreen(t, state)
			if search.Cells[10].Rune != '[' || search.Cells[16].Rune != ']' || search.Cells[11].BG == baseline.Cells[11].BG {
				t.Fatal("search brackets, active unmarked style, or tab positions changed")
			}
			rt.ApplyAction(ImportantTabAction{Reset: true})
			reset := importantTabScreen(t, rt.Read())
			for i := range 80 {
				if reset.Cells[i] != baseline.Cells[i] {
					t.Fatalf("reset did not restore original bar cell %d", i)
				}
			}
		})
	}
}

func TestImportantTabChangeUsesExistingFrameCache(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			rt := NewRuntime()
			rt.ApplyAction(SessionTabsAction{Input: SessionTabsInput{Sources: []SessionTabSource{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}, ActiveID: "a"}})
			snap := BlankSnapshot(80, 10)
			snap.Cursor = &protocolpb.Cursor{Y: 3}
			snap.CursorVisible = true
			copy(snap.Runes[80:160], []uint32{'S', 'T', 'A', 'B', 'L', 'E'})
			var cache RenderCache
			render := func() []byte {
				t.Helper()
				if mode == "host" {
					out, err := rt.RenderHostFrame(RuntimeHostFrameInput{Snapshot: snap, Cols: 80, Rows: 10, Cursor: Cursor{Row: 4, Col: 1, Visible: true}, Cache: &cache})
					if err != nil {
						t.Fatal(err)
					}
					return out.Rendered.Bytes
				}
				out, err := rt.RenderAttachFrame(RuntimeAttachFrameInput{Snapshot: snap, Cols: 80, Rows: 10, Cursor: Cursor{Row: 4, Col: 1, Visible: true}, Cache: &cache})
				if err != nil {
					t.Fatal(err)
				}
				return out.Rendered.Bytes
			}
			render()
			rt.ApplyAction(ImportantTabAction{SessionID: "a"})
			if data := string(render()); data == "" || strings.Contains(data, "\x1b[2J") || strings.Contains(data, "STABLE") {
				t.Fatalf("importance change cleared or repainted the session: %q", data)
			}
			// Ordinary live frames may restore the cursor; they must not repaint
			// unchanged tab text or terminal cells.
			if data := string(render()); regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]").ReplaceAllString(data, "") != "" || strings.Contains(data, "\x1b[2J") {
				t.Fatalf("unchanged model repainted content: %q", data)
			}
		})
	}
}
