package mvu

import (
	"strings"
	"testing"

	"pkt.systems/version"
)

func TestHelpHeader(t *testing.T) {
	lines := HelpLines(State{SessionID: "redundant-session", Endpoint: "https://relay.example/v1"})
	want := []string{"lingon " + version.Current(), "Copyright (C) 2026 C89 Systems AB https://c89.systems", "endpoint: https://relay.example/v1"}
	for i, line := range want {
		if lines[i] != line {
			t.Fatalf("help line %d=%q, want %q", i, lines[i], line)
		}
	}
	if strings.Contains(strings.Join(lines, "\n"), "redundant-session") {
		t.Fatal("help repeats the session name")
	}
}
