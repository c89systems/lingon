package control

import (
	"reflect"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestImportantPromptBatchEditing(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha.prod"}, {ID: "b", Name: "beta.build"}, {ID: "c", Name: "beta.test"}, {ID: "d", Name: "日本語_🦊"}}
	for _, tc := range []struct {
		keys, text string
		ids        []string
		active     bool
	}{
		{"\r", "alpha.prod ", []string{"a"}, false},
		{"beta.build 日本\r", "alpha.prod beta.build 日本", []string{"a", "b", "d"}, false},
		{"be\r", "alpha.prod be", nil, true},
		{"beta.build missing\r", "alpha.prod beta.build missing", nil, true},
		{"beta.build \t\r", "alpha.prod beta.build beta.build", []string{"a", "b"}, false},
		{"\t\t\r", "alpha.prod beta.test", []string{"a", "c"}, false},
		{"be\t 日本\t\r", "alpha.prod beta.build 日本語_🦊", []string{"a", "b", "d"}, false},
		{"\x1b[Z\r", "alpha.prod 日本語_🦊", []string{"a", "d"}, false},
		{"beta.build\x17", "alpha.prod ", nil, true},
		{"beta.build  \x17", "alpha.prod ", nil, true},
		{"beta.build\x17\x17", "", nil, true},
		{"日本語_🦊\x17", "alpha.prod ", nil, true},
		{"alpha.prod alpha.prod\r", "alpha.prod alpha.prod alpha.prod", []string{"a"}, false},
		{"beta.build\x03", "alpha.prod beta.build", nil, false},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			var prompt SearchPrompt
			prompt.BeginImportant(clock.NewMock(), nil, "a", sessions)
			defer prompt.Close()
			if view := prompt.View(sessions); view.Text != "alpha.prod " || !view.Important || !view.Active || view.CandidateID != "a" {
				t.Fatalf("prefill %+v", view)
			}
			var result SearchResult
			for _, b := range []byte(tc.keys) {
				result = prompt.Feed(b, sessions)
				prompt.View(sessions)
			}
			view := prompt.View(sessions)
			if view.Active != tc.active || view.Text != tc.text || !reflect.DeepEqual(result.SessionIDs, tc.ids) || result.SessionID != "" {
				t.Fatalf("view=%+v result=%+v want text=%q active=%v ids=%v", view, result, tc.text, tc.active, tc.ids)
			}
		})
	}
}

func TestImportantPromptIdentityAndAmbiguity(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "same"}, {ID: "b", Name: "same"}, {ID: "c", Name: "sameness"}}
	for _, tc := range []struct {
		keys   string
		ids    []string
		active bool
	}{
		{"\r", []string{"b"}, false},           // Initial current identity survives duplicate names.
		{"\x17same\r", nil, true},              // Manually typed duplicate exact names stay ambiguous.
		{"sam\t\r", []string{"b", "a"}, false}, // Tab pins the chosen duplicate.
		{"\t\r", []string{"b", "c"}, false},
		{"sam\r", nil, true},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			var p SearchPrompt
			p.BeginImportant(clock.NewMock(), nil, "b", sessions)
			defer p.Close()
			var result SearchResult
			for _, b := range []byte(tc.keys) {
				result = p.Feed(b, sessions)
				p.View(sessions)
			}
			if !reflect.DeepEqual(result.SessionIDs, tc.ids) || p.View(sessions).Active != tc.active {
				t.Fatalf("%+v %+v", result, p.View(sessions))
			}
		})
	}
	var p SearchPrompt
	p.BeginImportant(clock.NewMock(), nil, "b", sessions)
	defer p.Close()
	if result := p.Feed('\r', sessions[:1]); result.SessionIDs != nil || !p.View(sessions[:1]).Active {
		t.Fatal("missing pinned session retargeted by name")
	}
	p.BeginImportant(clock.NewMock(), nil, "a", sessions)
	p.Feed('\t', sessions)
	sessions[1].Name = "renamed"
	if result := p.Feed('\r', sessions); result.SessionIDs != nil {
		t.Fatal("renamed completed token accepted")
	}
}

func TestImportantPromptGhostEscapeAndModeReset(t *testing.T) {
	clk := clock.NewMock()
	var p SearchPrompt
	changed := make(chan struct{}, 1)
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}
	p.BeginImportant(clk, func() { changed <- struct{}{} }, "a", sessions)
	defer p.Close()
	p.Feed('b', sessions)
	if view := p.View(sessions); view.Ghost != "eta" || view.CandidateID != "b" {
		t.Fatalf("%+v", view)
	}
	p.Feed(0x1b, sessions)
	clk.Add(50 * time.Millisecond)
	<-changed
	if p.View(sessions).Active {
		t.Fatal("Escape did not cancel")
	}
	if result := p.Feed('x', sessions); result.Consumed {
		t.Fatal("cancel swallowed application input")
	}
	p.Begin(clk, nil, "a")
	if view := p.View(sessions); view.Important || view.Text != "" {
		t.Fatal("batch state leaked into ordinary search")
	}
	p.Feed('\t', sessions)
	if p.Feed('\r', sessions).SessionID != "b" {
		t.Fatal("ordinary search behavior changed")
	}
}

func TestImportantPromptTabCyclesUntilSpaceCommits(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}, {ID: "c", Name: "current"}, {ID: "d", Name: "delta"}}
	var p SearchPrompt
	p.BeginImportant(clock.NewMock(), nil, "c", sessions)
	defer p.Close()
	check := func(text, ghost, id string) {
		t.Helper()
		got := p.View(sessions)
		if got.Text != text || got.Ghost != ghost || got.CandidateID != id || !got.Active {
			t.Fatalf("view=%+v want text=%q ghost=%q candidate=%q", got, text, ghost, id)
		}
	}
	check("current ", "", "c")
	for _, tc := range []struct {
		key      byte
		name, id string
	}{{'\t', "delta", "d"}, {'\t', "alpha", "a"}, {'\t', "beta", "b"}, {'\t', "current", "c"}, {'\t', "delta", "d"}, {'\t', "alpha", "a"}} {
		p.Feed(tc.key, sessions)
		check("current "+tc.name, "", tc.id)
	}
	for _, b := range []byte("\x1b[Z") {
		p.Feed(b, sessions)
	}
	check("current delta", "", "d")
	p.Feed(' ', sessions)
	check("current delta ", "", "d")
	p.Feed('b', sessions)
	check("current delta b", "eta", "b")
	p.Feed('\t', sessions)
	check("current delta beta", "", "b")
	// Space also accepts the ghost of a typed prefix, preserving that exact ID.
	p.Feed(0x17, sessions)
	p.Feed('b', sessions)
	p.Feed(' ', sessions)
	check("current delta beta ", "", "b")
	p.Feed('\t', sessions)
	check("current delta beta delta", "", "d")
	p.Feed('\t', sessions)
	check("current delta beta alpha", "", "a")
	result := p.Feed('\r', sessions)
	if !reflect.DeepEqual(result.SessionIDs, []string{"c", "d", "b", "a"}) || p.View(sessions).Active {
		t.Fatalf("accepted %+v", result)
	}
}

func TestImportantEmptyEntryCyclesRelativeToActiveTab(t *testing.T) {
	for _, tc := range []struct{ active, keys, want string }{
		{"c", "\t", "delta"}, {"c", "\x1b[Z", "beta"},
		{"a", "\x1b[Z", "delta"}, {"d", "\t", "alpha"},
	} {
		t.Run(tc.active+tc.keys, func(t *testing.T) {
			sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}, {ID: "c", Name: "current"}, {ID: "d", Name: "delta"}}
			var p SearchPrompt
			p.BeginImportant(clock.NewMock(), nil, tc.active, sessions)
			defer p.Close()
			for _, b := range []byte(tc.keys) {
				p.Feed(b, sessions)
			}
			if got := importantToken(p.View(sessions).Text); got != tc.want {
				t.Fatalf("first completion=%q want %q", got, tc.want)
			}
			p.Feed(' ', sessions)
			// Starting another empty entry keeps the active-tab origin, rather than
			// using the last accepted list entry as the origin.
			for _, b := range []byte(tc.keys) {
				p.Feed(b, sessions)
			}
			if got := importantToken(p.View(sessions).Text); got != tc.want {
				t.Fatalf("next empty completion=%q want %q", got, tc.want)
			}
		})
	}
}

func TestImportantMarkedParentStartsEmptyWithoutLosingCycleOrigin(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}, {ID: "c", Name: "current", Marked: true}, {ID: "d", Name: "delta"}}
	for _, tc := range []struct {
		keys string
		ids  []string
	}{
		{"\r", []string{}}, {"\t\r", []string{"d"}}, {"\x1b[Z\r", []string{"b"}},
		{"delta\r", []string{"d"}}, {"current\r", []string{"c"}}, {"delta current\r", []string{"d", "c"}},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			var p SearchPrompt
			p.BeginImportant(clock.NewMock(), nil, "c", sessions)
			defer p.Close()
			if view := p.View(sessions); !view.Active || !view.Important || view.Text != "" || view.Ghost != "" || view.CandidateID != "" {
				t.Fatalf("marked prefill=%+v", view)
			}
			var result SearchResult
			for _, key := range []byte(tc.keys) {
				result = p.Feed(key, sessions)
				p.View(sessions)
			}
			if p.View(sessions).Active || !reflect.DeepEqual(result.SessionIDs, tc.ids) {
				t.Fatalf("result=%+v view=%+v", result, p.View(sessions))
			}
		})
	}
}

func TestImportantPrefillUsesMarkIdentityRatherThanName(t *testing.T) {
	sessions := []SearchSession{{ID: "a", Name: "same", Marked: true}, {ID: "b", Name: "same"}}
	var p SearchPrompt
	p.BeginImportant(clock.NewMock(), nil, "b", sessions)
	defer p.Close()
	if view := p.View(sessions); view.Text != "same " || view.CandidateID != "b" {
		t.Fatalf("unmarked duplicate prefill=%+v", view)
	}
	p.BeginImportant(clock.NewMock(), nil, "a", sessions)
	for _, key := range []byte("same\r") {
		p.Feed(key, sessions)
	}
	if !p.View(sessions).Active {
		t.Fatal("empty marked prefill retained an implicit duplicate identity")
	}
}
