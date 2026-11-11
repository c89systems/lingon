package mvu

import (
	"strings"
	"testing"
	"time"
)

func TestSearchFieldCompositionAndRemoval(t *testing.T) {
	const cols, rows = 40, 6
	rt := NewRuntime()
	base := BlankSnapshot(cols, rows)
	for i := (rows - 1) * cols; i < rows*cols; i++ {
		base.Runes[i] = 'x'
	}
	rt.ApplyAction(SearchAction{Search: SearchState{Visible: true, Text: "al", Ghost: "pha"}})
	resolved := Resolve(rt.Read(), Cursor{Row: 2, Col: 1, Visible: true}, time.Time{}, ResolveOptions{})
	snap, err := ComposeViewportSnapshot(base, cols, rows, resolved, Cursor{Row: 2, Col: 1, Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	var bottom strings.Builder
	for _, r := range snap.Runes[(rows-1)*cols:] {
		bottom.WriteRune(rune(r))
	}
	if !strings.HasPrefix(bottom.String(), "/ alpha") {
		t.Fatalf("bottom %q", bottom.String())
	}
	if snap.Fg[(rows-1)*cols+3] == snap.Fg[(rows-1)*cols+4] {
		t.Fatal("completion did not use gray foreground")
	}
	if snap.Cursor.Y != rows-1 || snap.Cursor.X != 4 {
		t.Fatalf("cursor %v", snap.Cursor)
	}
	rt.ApplyAction(SearchAction{})
	resolved = Resolve(rt.Read(), Cursor{}, time.Time{}, ResolveOptions{})
	snap, err = ComposeViewportSnapshot(base, cols, rows, resolved, Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range snap.Runes[(rows-1)*cols:] {
		if r != 'x' {
			t.Fatal("underlying last row not restored")
		}
	}
}

func TestSearchFieldFitsNarrowViewport(t *testing.T) {
	for _, cols := range []int{1, 2, 3, 5, 10} {
		state := NewRuntime().Read()
		state.Search = SearchState{Visible: true, Text: "日本語long", Ghost: "more"}
		resolved := Resolve(state, Cursor{}, time.Time{}, ResolveOptions{})
		snap, err := ComposeViewportSnapshot(BlankSnapshot(cols, 2), cols, 2, resolved, Cursor{})
		if err != nil {
			t.Fatal(err)
		}
		if int(snap.Cursor.X) >= cols || snap.Cursor.Y != 1 {
			t.Fatalf("cols %d cursor %v", cols, snap.Cursor)
		}
	}
}

func TestSearchLongNameKeepsTypedSuffixVisible(t *testing.T) {
	state := NewRuntime().Read()
	state.Search = SearchState{Visible: true, Text: "long-session-name-tail"}
	resolved := Resolve(state, Cursor{}, time.Time{}, ResolveOptions{})
	snap, err := ComposeViewportSnapshot(BlankSnapshot(10, 2), 10, 2, resolved, Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	var line strings.Builder
	for _, r := range snap.Runes[10:] {
		line.WriteRune(rune(r))
	}
	if !strings.Contains(line.String(), "name-tail") {
		t.Fatalf("typed suffix hidden: %q", line.String())
	}
}

func TestImportantPromptCompositionAndClipping(t *testing.T) {
	for _, cols := range []int{1, 2, 3, 5, 10, 40} {
		state := NewRuntime().Read()
		state.Search = SearchState{Visible: true, Important: true, Text: "alpha 日本語", Ghost: "_build"}
		resolved := Resolve(state, Cursor{}, time.Time{}, ResolveOptions{})
		snap, err := ComposeViewportSnapshot(BlankSnapshot(cols, 3), cols, 3, resolved, Cursor{})
		if err != nil {
			t.Fatal(err)
		}
		if snap.Cursor.Y != 2 || int(snap.Cursor.X) >= cols {
			t.Fatalf("cols=%d cursor=%v", cols, snap.Cursor)
		}
		if cols == 40 && (snap.Runes[2*cols] != 'i' || snap.Runes[2*cols+1] != ' ') {
			t.Fatal("importance prompt has incorrect prefix")
		}
	}
}

func TestDirectoryFieldPrefixCursorAndClipping(t *testing.T) {
	for _, cols := range []int{1, 5, 20, 80} {
		state := NewRuntime().Read()
		state.Search = SearchState{Visible: true, Label: "working directory: ", Text: "/owner/日本語", Ghost: "_build/", Hint: "loading…"}
		resolved := Resolve(state, Cursor{}, time.Time{}, ResolveOptions{})
		snap, err := ComposeViewportSnapshot(BlankSnapshot(cols, 4), cols, 4, resolved, Cursor{})
		if err != nil {
			t.Fatal(err)
		}
		if int(snap.Cursor.X) >= cols || snap.Cursor.Y != 3 {
			t.Fatalf("%d: %v", cols, snap.Cursor)
		}
		if cols == 80 {
			if string(rune(snap.Runes[3*cols])) != "w" || snap.Cursor.X != 32 {
				t.Fatalf("prefix/cursor: %v", snap.Cursor)
			}
		}
	}
}
