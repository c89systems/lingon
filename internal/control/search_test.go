package control

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
)

func TestSearchCompletionAndCycling(t *testing.T) {
	sessions := []SearchSession{{"a", "alpha"}, {"b", "alpine"}, {"c", "beta"}, {"d", "beta"}}
	var p SearchPrompt
	p.Begin(clock.NewMock())
	for _, b := range []byte("al") {
		p.Feed(b, sessions)
	}
	if text, ghost := p.Display(sessions); text != "al" || ghost != "pha" {
		t.Fatalf("display %q %q", text, ghost)
	}
	p.Feed('\t', sessions)
	if text, ghost := p.Display(sessions); text != "alpha" || ghost != "" {
		t.Fatalf("completed %q %q", text, ghost)
	}
	for _, want := range []string{"alpine", "beta", "beta", "alpha"} {
		p.Feed('\t', sessions)
		if text, _ := p.Display(sessions); text != want {
			t.Fatalf("cycle %q, want %q", text, want)
		}
	}
	if id, done := p.Feed('\r', sessions); id != "a" || !done || p.Active {
		t.Fatalf("selection %q %v", id, done)
	}
}

func TestSearchEnterExactGhostDuplicateAndMissing(t *testing.T) {
	sessions := []SearchSession{{"long", "alphabet"}, {"exact", "alpha"}, {"dup", "alpha"}, {"unicode", "räv 🦊"}}
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
			p.Begin(clock.NewMock())
			var id string
			var done bool
			for _, b := range []byte(tc.keys) {
				id, done = p.Feed(b, sessions)
			}
			if id != tc.id || done != tc.done || p.Active == done {
				t.Fatalf("got %q %v active=%v", id, done, p.Active)
			}
		})
	}
}

func TestSearchRefreshAndSafeText(t *testing.T) {
	var p SearchPrompt
	p.Begin(clock.NewMock())
	sessions := []SearchSession{{"a", "alpha"}, {"b", "beta"}}
	p.Feed('\t', sessions)
	if id, done := p.Feed('\r', sessions[1:]); done || id != "" {
		t.Fatalf("selected removed session %q", id)
	}
	p.Begin(clock.NewMock())
	p.Feed('\t', []SearchSession{{"a", "a\x1b[2J\n"}})
	if text, _ := p.Display(nil); text != "a[2J" {
		t.Fatalf("unsafe text %q", text)
	}
	p.Begin(clock.NewMock())
	p.Feed('\t', nil)
	if id, done := p.Feed('\r', nil); done || id != "" {
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
	prompt.Begin(clock.NewMock())
	if prompt.CandidateID(items) != "" {
		t.Fatal("empty search highlighted a session")
	}
	for _, b := range []byte("al") {
		prompt.Feed(b, items)
	}
	if prompt.CandidateID(items) != "a" {
		t.Fatal("prefix candidate missing")
	}
	prompt.Feed('\t', items)
	prompt.Feed('\t', items)
	if prompt.CandidateID(items) != "b" {
		t.Fatal("duplicate name lost candidate identity")
	}
	if prompt.CandidateID(items[:1]) != "a" {
		t.Fatal("removed candidate retained")
	}
	prompt.Feed('z', items)
	if prompt.CandidateID(items) != "" {
		t.Fatal("no-match search retained a highlight")
	}
	prompt.Feed('\x1b', items)
	if prompt.CandidateID(items) != "" {
		t.Fatal("cancelled search retained a highlight")
	}
}
