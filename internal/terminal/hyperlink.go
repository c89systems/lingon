package terminal

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeHyperlink accepts an OSC 8 params;URI payload without terminal control
// characters. Empty URIs close the link. It never derives links from text styles.
func NormalizeHyperlink(payload string) string {
	_, uri, ok := strings.Cut(payload, ";")
	if !ok || uri == "" || !utf8.ValidString(payload) {
		return ""
	}
	for _, r := range payload {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return payload
}

// HyperlinkSequence opens a validated OSC 8 target, or closes the current link.
func HyperlinkSequence(payload string) string {
	payload = NormalizeHyperlink(payload)
	if payload == "" {
		payload = ";"
	}
	return "\x1b]8;" + payload + "\x1b\\"
}
