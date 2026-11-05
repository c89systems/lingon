package control

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestSearchCompletionAndCycling(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "alpine"}, {ID: "c", Name: "beta"}, {ID: "d", Name: "beta"}}
	var p SearchPrompt
	p.Begin(clock.NewMock(), nil, "")
	defer p.Close()
	for _, b := range []byte("al") {
		p.Feed(b, sessions)
	}
	if view := p.View(sessions); view.Text != "al" || view.Ghost != "pha" {
		t.Fatalf("display %+v", view)
	}
	p.Feed('\t', sessions)
	if view := p.View(sessions); view.Text != "alpha" || view.Ghost != "" {
		t.Fatalf("completed %+v", view)
	}
	for _, want := range []string{"alpine", "beta", "beta", "alpha"} {
		p.Feed('\t', sessions)
		if view := p.View(sessions); view.Text != want {
			t.Fatalf("cycle %q, want %q", view.Text, want)
		}
	}
	if result := p.Feed('\r', sessions); result.SessionID != "a" || !result.Consumed || p.View(sessions).Active {
		t.Fatalf("selection %+v", result)
	}
}

func TestEmptySearchCyclesRelativeToOrigin(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "same"}, {ID: "c", Name: "same"}, {ID: "d", Name: "delta"}}
	for _, tc := range []struct{ origin, keys, want string }{
		{"b", "\t", "c"}, {"b", "\x1b[Z", "a"},
		{"d", "\t", "a"}, {"a", "\x1b[Z", "d"},
		{"b", "\t\x1b[Z", "b"}, {"b", "\x1b[Z\t", "b"},
		{"removed", "\t", "a"}, {"removed", "\x1b[Z", "d"},
	} {
		t.Run(tc.origin+tc.keys, func(t *testing.T) {
			var p SearchPrompt
			p.Begin(clock.NewMock(), nil, tc.origin)
			defer p.Close()
			if view := p.View(sessions); view.CandidateID != "" || view.Text != "" {
				t.Fatal("empty search previewed the origin")
			}
			for _, b := range []byte(tc.keys) {
				p.Feed(b, sessions)
			}
			if got := p.Feed('\r', sessions).SessionID; got != tc.want {
				t.Fatalf("selected %s, want %s", got, tc.want)
			}
		})
	}
	var p SearchPrompt
	p.Begin(clock.NewMock(), nil, "a")
	defer p.Close()
	p.Feed('\t', sessions[:1])
	if got := p.Feed('\r', sessions[:1]).SessionID; got != "a" {
		t.Fatal("single session failed to wrap to itself")
	}
}

func TestSearchEnterExactGhostDuplicateAndMissing(t *testing.T) {
	sessions := []SearchSession{{ID: "long", Name: "alphabet"}, {ID: "exact", Name: "alpha"}, {ID: "dup", Name: "alpha"}, {ID: "unicode", Name: "räv 🦊"}}
	for _, tc := range []struct {
		keys, id string
		done     bool
	}{
		{"alp\r", "long", true},
		{"alpha\r", "exact", true},
		{"alpha\t\r", "dup", true},
		{"\t\r", "long", true},
		{"räv\x7f\r", "unicode", true},
		{"zzz\r", "", false},
		{"\r", "", false},
		{"alp\x1b", "", true},
		{"alp\x03", "", true},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			var p SearchPrompt
			clk := clock.NewMock()
			changed := make(chan struct{}, 1)
			p.Begin(clk, func() { changed <- struct{}{} }, "")
			defer p.Close()
			var result SearchResult
			for _, b := range []byte(tc.keys) {
				result = p.Feed(b, sessions)
			}
			if tc.keys == "alp\x1b" {
				clk.Add(searchEscapeDelay)
				<-changed
			}
			view := p.View(sessions)
			if result.SessionID != tc.id || view.Active == tc.done {
				t.Fatalf("got %+v active=%v", result, view.Active)
			}
		})
	}
}

func TestSearchRefreshAndSafeText(t *testing.T) {
	var p SearchPrompt
	p.Begin(clock.NewMock(), nil, "")
	defer p.Close()
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}
	p.Feed('\t', sessions)
	if result := p.Feed('\r', sessions[1:]); result.SessionID != "" || !p.View(sessions[1:]).Active {
		t.Fatalf("selected removed session %+v", result)
	}
	p.Begin(clock.NewMock(), nil, "")
	defer p.Close()
	p.Feed('\t', []SearchSession{{ID: "a", Name: "a\x1b[2J\n"}})
	if view := p.View(nil); view.Text != "a[2J" {
		t.Fatalf("unsafe text %q", view.Text)
	}
	p.Begin(clock.NewMock(), nil, "")
	defer p.Close()
	p.Feed('\t', nil)
	if result := p.Feed('\r', nil); result.SessionID != "" || !p.View(nil).Active {
		t.Fatal("empty list accepted")
	}
}

func TestSearchAndRestorePrefix(t *testing.T) {
	for key, want := range map[byte]Action{'/': ActionSearch, '!': ActionToggleRestore} {
		var p Prefix
		p.Feed(CtrlL)
		if action, out := p.Feed(key); action != want || len(out) != 0 {
			t.Fatalf("key %q: %v %q", key, action, out)
		}
	}
}

func TestSearchCandidateIdentityFollowsEditingAndDuplicateCycling(t *testing.T) {
	items := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "alpha"}, {ID: "c", Name: "beta"}}
	var prompt SearchPrompt
	prompt.Begin(clock.NewMock(), nil, "")
	defer prompt.Close()
	if prompt.View(items).CandidateID != "" {
		t.Fatal("empty search highlighted a session")
	}
	for _, b := range []byte("al") {
		prompt.Feed(b, items)
	}
	if prompt.View(items).CandidateID != "a" {
		t.Fatal("prefix candidate missing")
	}
	prompt.Feed('\t', items)
	prompt.Feed('\t', items)
	if prompt.View(items).CandidateID != "b" {
		t.Fatal("duplicate name lost candidate identity")
	}
	if prompt.View(items[:1]).CandidateID != "a" {
		t.Fatal("removed candidate retained")
	}
	prompt.Feed('z', items)
	if prompt.View(items).CandidateID != "" {
		t.Fatal("no-match search retained a highlight")
	}
	prompt.Feed('\x03', items)
	if prompt.View(items).CandidateID != "" {
		t.Fatal("cancelled search retained a highlight")
	}
}

func TestSearchReverseCycling(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "alpha"}, {ID: "c", Name: "beta"}, {ID: "d", Name: "räv 🦊"}}
	for _, tc := range []struct {
		keys string
		ids  []string
	}{
		{"", []string{"d", "c", "b", "a", "d"}},
		{"al", []string{"a", "d", "c", "b", "a"}},
		{"alpha", []string{"d", "c", "b", "a", "d"}},
		{"missing", []string{"d", "c", "b", "a", "d"}},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			var p SearchPrompt
			clk := clock.NewMock()
			p.Begin(clk, nil, "")
			defer p.Close()
			for _, b := range []byte(tc.keys) {
				p.Feed(b, sessions)
			}
			for _, id := range tc.ids {
				for _, b := range []byte("\x1b[Z") {
					if result := p.Feed(b, sessions); !result.Consumed || result.SessionID != "" {
						t.Fatalf("unexpected result %+v", result)
					}
				}
				clk.Add(searchEscapeDelay)
				view := p.View(sessions)
				if !view.Active || view.CandidateID != id || view.Ghost != "" {
					t.Fatalf("view %+v, want %s", view, id)
				}
			}
			// Switching direction must retain identity even for duplicate names.
			p.Feed('\t', sessions)
			want := "a"
			if tc.keys == "al" {
				want = "b"
			}
			if p.View(sessions).CandidateID != want {
				t.Fatalf("forward after reverse: %+v", p.View(sessions))
			}
		})
	}
}

func TestSearchShiftTabFragmentedInput(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}
	for split := 1; split < 3; split++ {
		t.Run(string(rune('0'+split)), func(t *testing.T) {
			clk := clock.NewMock()
			var p SearchPrompt
			p.Begin(clk, func() { t.Error("Shift+Tab cancelled search") }, "")
			defer p.Close()
			for _, b := range []byte("\x1b[Z")[:split] {
				p.Feed(b, sessions)
			}
			clk.Add(searchEscapeDelay - time.Nanosecond)
			if !p.View(sessions).Active {
				t.Fatal("cancelled before escape deadline")
			}
			for _, b := range []byte("\x1b[Z")[split:] {
				p.Feed(b, sessions)
			}
			clk.Add(time.Second)
			if view := p.View(sessions); !view.Active || view.CandidateID != "b" {
				t.Fatalf("fragmented sequence: %+v", view)
			}
		})
	}
}

func TestSearchEscapeCancellation(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b["} {
		t.Run(prefix, func(t *testing.T) {
			clk := clock.NewMock()
			changed := make(chan struct{}, 1)
			var p SearchPrompt
			p.Begin(clk, func() { changed <- struct{}{} }, "")
			defer p.Close()
			for _, b := range []byte(prefix) {
				p.Feed(b, nil)
			}
			clk.Add(searchEscapeDelay - time.Nanosecond)
			if !p.View(nil).Active {
				t.Fatal("cancelled early")
			}
			clk.Add(time.Nanosecond)
			<-changed // Callback completion is the barrier, not a wall-clock sleep.
			if p.View(nil).Active || !p.HandlesInput() {
				t.Fatal("Escape failed to cancel and retain tail handling")
			}
			tail := "[A"
			if prefix == "\x1b[" {
				tail = "A"
			}
			for _, b := range []byte(tail) {
				if !p.Feed(b, nil).Consumed {
					t.Fatal("escape tail leaked")
				}
			}
			if p.Feed('x', nil).Consumed {
				t.Fatal("ordinary input swallowed after Escape")
			}
		})
	}
}

func TestSearchOtherEscapeSequences(t *testing.T) {
	for _, keys := range []string{"\x1b[A", "\x1bOA", "\x1b[1;2D"} {
		t.Run(keys, func(t *testing.T) {
			var p SearchPrompt
			p.Begin(clock.NewMock(), nil, "")
			defer p.Close()
			for _, b := range []byte(keys) {
				if !p.Feed(b, nil).Consumed {
					t.Fatal("escape byte leaked")
				}
			}
			if p.View(nil).Active {
				t.Fatal("arrow did not cancel")
			}
			if p.Feed('x', nil).Consumed {
				t.Fatal("ordinary input swallowed")
			}
		})
	}
	var p SearchPrompt
	p.Begin(clock.NewMock(), nil, "")
	defer p.Close()
	p.Feed(0x1b, nil)
	if r := p.Feed('x', nil); r.Consumed || !r.Changed || p.View(nil).Active {
		t.Fatalf("plain key after Escape: %+v", r)
	}
}

func TestSearchEscapeTimerLifecycle(t *testing.T) {
	clk := clock.NewMock()
	var p SearchPrompt
	p.Begin(clk, func() { t.Error("old Escape callback fired") }, "")
	p.Feed(0x1b, nil)
	p.Begin(clk, nil, "")
	clk.Add(time.Second)
	if !p.View(nil).Active {
		t.Fatal("old timer cancelled new search")
	}
	p.Begin(clk, func() { t.Error("closed search callback fired") }, "")
	p.Feed(0x1b, nil)
	p.Close()
	clk.Add(time.Second)
	if p.HandlesInput() {
		t.Fatal("closed search consumes input")
	}
}
