package terminal

import "testing"

func TestTextPasteModeAndNewlines(t *testing.T) {
	if got := string(TextPaste("one\ntwo\n", 0)); got != "one\rtwo\r" {
		t.Fatalf("plain paste=%q", got)
	}
	if got := string(TextPaste("one\ntwo\n", SnapshotModeBracketedPaste)); got != "\x1b[200~one\ntwo\n\x1b[201~" {
		t.Fatalf("bracketed paste=%q", got)
	}
	if len(TextPaste("", SnapshotModeBracketedPaste)) != 0 {
		t.Fatal("empty clipboard generated input")
	}
}
