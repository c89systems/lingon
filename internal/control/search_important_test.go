package control

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
)

func TestImportantSearchKeepsFullTabOrderAndSkipsExcluded(t *testing.T) {
	items := []SearchSession{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta", Excluded: true}, {ID: "c", Name: "charlie"}, {ID: "d", Name: "delta", Excluded: true}}
	for _, tc := range []struct{ origin, keys, want string }{
		{"b", "\t", "c"}, {"b", "\x1b[Z", "a"},
		{"a", "\x1b[Z", "c"}, {"c", "\t", "a"},
		{"d", "\t", "a"}, {"d", "\x1b[Z", "c"},
		{"b", "\t\t", "a"}, {"c", "a\r", "a"},
		{"b", "beta\r", ""}, {"a", "d\r", ""},
	} {
		t.Run(tc.origin+tc.keys, func(t *testing.T) {
			var prompt SearchPrompt
			prompt.Begin(clock.NewMock(), nil, tc.origin)
			defer prompt.Close()
			var selected string
			for _, b := range []byte(tc.keys) {
				selected = prompt.Feed(b, items).SessionID
			}
			if selected == "" && tc.want != "" {
				selected = prompt.Feed('\r', items).SessionID
			}
			if selected != tc.want {
				t.Fatalf("selected %q, want %q; view=%+v", selected, tc.want, prompt.View(items))
			}
			if tc.want == "" && (!prompt.View(items).Active || prompt.View(items).Ghost != "") {
				t.Fatal("excluded prefix completed or closed search")
			}
		})
	}
}

func TestImportantSearchDuplicateIdentityAndEmptySubset(t *testing.T) {
	items := []SearchSession{{ID: "old", Name: "same", Excluded: true}, {ID: "a", Name: "same"}, {ID: "b", Name: "same"}}
	var prompt SearchPrompt
	prompt.Begin(clock.NewMock(), nil, "old")
	defer prompt.Close()
	for _, b := range []byte("same\t\r") {
		result := prompt.Feed(b, items)
		if b == '\r' && result.SessionID != "b" {
			t.Fatalf("duplicate selected %q, want b", result.SessionID)
		}
	}
	prompt.Begin(clock.NewMock(), nil, "a")
	for i := range items {
		items[i].Excluded = true
	}
	for _, b := range []byte("\t\x1b[Zsame\r") {
		if result := prompt.Feed(b, items); result.SessionID != "" {
			t.Fatal("empty subset selected a tab")
		}
	}
	if view := prompt.View(items); !view.Active || view.CandidateID != "" || view.Ghost != "" {
		t.Fatalf("empty subset view=%+v", view)
	}
}
