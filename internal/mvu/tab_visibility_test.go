package mvu

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func tabVisibilityRow(t *testing.T, state State, cols int) string {
	t.Helper()
	cursor := Cursor{Row: 2, Col: 1, Visible: true}
	snapshot, err := ComposeViewportSnapshot(BlankSnapshot(cols, 4), cols, 4, Resolve(state, cursor, time.Time{}, ResolveOptions{}), cursor)
	if err != nil {
		t.Fatal(err)
	}
	var row strings.Builder
	for _, r := range snapshot.Runes[:cols] {
		if r != 0 {
			row.WriteRune(rune(r))
		}
	}
	return row.String()
}

func TestTabViewportFollowsActiveAndHighlightedCells(t *testing.T) {
	state := NewRuntime().Read()
	for i := 0; i < 8; i++ {
		state.Tabs = append(state.Tabs, Tab{ID: fmt.Sprint(i), Index: i + 1, Title: fmt.Sprintf("tab-%d", i)})
	}
	for _, cols := range []int{9, 16, 28, 80} {
		for focus := range state.Tabs {
			state.ActiveTab = focus
			state.Search = SearchState{}
			row := tabVisibilityRow(t, state, cols)
			if !strings.Contains(row, state.Tabs[focus].Title) {
				t.Fatalf("width %d active %d hidden: %q", cols, focus, row)
			}
			state.ActiveTab = 0
			state.Search = SearchState{Visible: true, CandidateID: state.Tabs[focus].ID}
			row = tabVisibilityRow(t, state, cols)
			if !strings.Contains(row, "["+state.Tabs[focus].Title+"]") {
				t.Fatalf("width %d candidate %d hidden: %q", cols, focus, row)
			}
		}
	}
}

func TestSearchHighlightUsesIdentityAndDoesNotMutateTabs(t *testing.T) {
	state := NewRuntime().Read()
	state.Tabs, _ = BuildSessionTabs([]SessionTabSource{{ID: "a", Name: "origin"}, {ID: "b", Name: "duplicate"}, {ID: "c", Name: "duplicate"}}, "a", BuildSessionTabsOptions{})
	state.Search = SearchState{Visible: true, CandidateID: "c"}
	preview := Resolve(state, Cursor{}, time.Time{}, ResolveOptions{})
	if preview.State.Tabs[0].Highlighted || preview.State.Tabs[1].Highlighted || !preview.State.Tabs[2].Highlighted {
		t.Fatal("candidate selected by name instead of ID")
	}
	for _, tab := range state.Tabs {
		if tab.Highlighted {
			t.Fatal("preview mutated shared tab state")
		}
	}
	if preview.State.ActiveTab != 0 {
		t.Fatal("preview changed active tab")
	}
	before := tabBarSignature(28, preview.State)
	state.Search.CandidateID = "b"
	if after := tabBarSignature(28, Resolve(state, Cursor{}, time.Time{}, ResolveOptions{}).State); after == before {
		t.Fatal("duplicate-name candidate did not invalidate tab render")
	}
	state.Search.CandidateID = "removed"
	if row := tabVisibilityRow(t, state, 18); strings.Contains(row, "[") || !strings.Contains(row, "origin") {
		t.Fatalf("missing candidate did not fall back to active tab: %q", row)
	}
	state.Search = SearchState{}
	if row := tabVisibilityRow(t, state, 18); strings.Contains(row, "[") || !strings.Contains(row, "origin") {
		t.Fatalf("cancel did not restore active tab: %q", row)
	}
}

func TestSearchTemporarilyShowsHiddenTabsWithoutStatusOcclusion(t *testing.T) {
	runtime := NewRuntime()
	state := runtime.Read()
	state.Tabs = []Tab{{ID: "a", Title: "alpha"}, {ID: "b", Title: "beta"}}
	state.TabBarVisible = false
	state.ConnectionMessage = "connection badge"
	state.LoadingMessage = "loading badge"
	state.Search = SearchState{Visible: true, CandidateID: "b"}
	cursor := Cursor{Row: 1, Col: 1, Visible: true}
	renderState, opts, delay := runtime.PrepareRenderState(state, cursor, time.Time{}, RenderStateOptions{SuppressTabs: true})
	resolved := Resolve(renderState, cursor, time.Time{}, opts)
	if !resolved.TabBarVisible || resolved.ConnectionVisible || resolved.LoadingVisible || delay != 0 {
		t.Fatalf("search candidate obscured: %+v", resolved)
	}
	if row := tabVisibilityRow(t, state, 18); !strings.Contains(row, "[beta]") {
		t.Fatal(row)
	}
	state.Search = SearchState{}
	if Resolve(state, cursor, time.Time{}, ResolveOptions{}).TabBarVisible {
		t.Fatal("search changed hidden-tab preference")
	}
}

func TestTabSegmentsFitUnicodeAndNarrowViewports(t *testing.T) {
	for _, name := range []string{"日本語", "räv 🦊", "e\u0301long-session-name", "long-long-long-session"} {
		for cols := 1; cols <= 24; cols++ {
			segment := tabSegment(Tab{Title: name, Highlighted: true}, cols)
			if !utf8.ValidString(segment) || runewidth.StringWidth(segment) > cols {
				t.Fatalf("width %d name %q: invalid segment %q", cols, name, segment)
			}
			if cols >= 2 && (!strings.HasPrefix(segment, "[") || !strings.HasSuffix(segment, "]")) {
				t.Fatalf("truncated highlight lost brackets: %q", segment)
			}
		}
	}
	state := NewRuntime().Read()
	state.Tabs = []Tab{{ID: "a", Title: "日本語"}, {ID: "b", Title: "second"}, {ID: "c", Title: "last"}}
	state.ActiveTab = 2
	if row := tabVisibilityRow(t, state, 16); !strings.Contains(row, "last") {
		t.Fatalf("wide preceding name hid active tab: %q", row)
	}
}

func TestTabHighlightPreservesLabelAndNeighborColumns(t *testing.T) {
	state := NewRuntime().Read()
	state.Tabs = []Tab{{ID: "a", Title: "alpha"}, {ID: "b", Title: "beta"}, {ID: "c", Title: "gamma"}}
	// All three tabs exactly fill the row. Highlight must not cause a scroll.
	state.ActiveTab = 2
	before := tabVisibilityRow(t, state, 20)
	for _, candidate := range state.Tabs {
		state.Search = SearchState{Visible: true, CandidateID: candidate.ID}
		after := tabVisibilityRow(t, state, 20)
		for _, tab := range state.Tabs {
			if want, got := strings.Index(before, tab.Title), strings.Index(after, tab.Title); want < 0 || want != got {
				t.Fatalf("highlight %s shifted %s: %d -> %d\nbefore %q\nafter %q", candidate.Title, tab.Title, want, got, before, after)
			}
		}
		if !strings.Contains(after, "["+candidate.Title+"]") {
			t.Fatal(after)
		}
	}
	for _, title := range []string{"日本語", "räv 🦊", "long-session-name"} {
		for cols := 2; cols <= 24; cols++ {
			plain := tabSegment(Tab{Title: title}, cols)
			highlight := tabSegment(Tab{Title: title, Highlighted: true}, cols)
			if plain[1:len(plain)-1] != highlight[1:len(highlight)-1] || runewidth.StringWidth(plain) != runewidth.StringWidth(highlight) {
				t.Fatalf("width %d: highlight changed title or width: %q -> %q", cols, plain, highlight)
			}
		}
	}
}
