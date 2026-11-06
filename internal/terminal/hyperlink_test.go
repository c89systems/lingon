package terminal

import "testing"

func TestHyperlinkPayloadValidation(t *testing.T) {
	for _, value := range []string{"", "missing-separator", "id=x;", ";https://example.invalid/\x1b[2J", ";https://example.invalid/\a", ";https://example.invalid/\n", ";bad\xff"} {
		if NormalizeHyperlink(value) != "" || HyperlinkSequence(value) != "\x1b]8;;\x1b\\" {
			t.Fatalf("unsafe or closed link accepted: %q", value)
		}
	}
	for _, value := range []string{";https://example.invalid/a;b", "id=x:other=y;file://host/tmp/a", ";mailto:me@example.invalid"} {
		if NormalizeHyperlink(value) != value || HyperlinkSequence(value) != "\x1b]8;"+value+"\x1b\\" {
			t.Fatalf("valid OSC8 payload changed: %q", value)
		}
	}
}
