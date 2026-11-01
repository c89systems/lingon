package control

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"pkt.systems/lingon/internal/clock"
)

// SearchSession identifies one searchable tab. Names need not be unique.
type SearchSession struct {
	ID, Name string
}

// SearchPrompt edits a prefix and selects sessions without changing the active tab.
type SearchPrompt struct {
	RenamePrompt
	selected  string
	completed bool
}

// Begin starts a fresh search. Escape handling shares the rename editor's parser.
func (p *SearchPrompt) Begin(clk clock.Clock) {
	*p = SearchPrompt{}
	p.RenamePrompt.Begin("", clk)
}

func printableName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(name, ""))
}

func (p *SearchPrompt) candidate(sessions []SearchSession) (SearchSession, bool) {
	text := string(p.text)
	for _, session := range sessions {
		if session.ID == p.selected && strings.HasPrefix(printableName(session.Name), text) {
			return session, true
		}
	}
	if text == "" || !utf8.Valid(p.text) {
		return SearchSession{}, false
	}
	// An exact name takes precedence over a longer name with the same prefix.
	for _, session := range sessions {
		if printableName(session.Name) == text {
			return session, true
		}
	}
	for _, session := range sessions {
		if strings.HasPrefix(printableName(session.Name), text) {
			return session, true
		}
	}
	return SearchSession{}, false
}

// Display returns entered text and the gray completion suffix.
func (p *SearchPrompt) Display(sessions []SearchSession) (string, string) {
	text := printableName(string(p.text))
	if session, ok := p.candidate(sessions); ok {
		p.selected = session.ID
		return text, strings.TrimPrefix(printableName(session.Name), text)
	}
	return text, ""
}

// Feed consumes an editing key. Enter returns the selected immutable session ID;
// cancellation returns an empty ID. A missing match leaves the field open.
func (p *SearchPrompt) Feed(b byte, sessions []SearchSession) (string, bool) {
	if b == '\r' || b == '\n' {
		if session, ok := p.candidate(sessions); ok {
			p.Active = false
			return session.ID, true
		}
		return "", false
	}
	if b == '\t' {
		if len(sessions) == 0 {
			return "", false
		}
		candidate, ok := p.candidate(sessions)
		if ok && !p.completed && printableName(candidate.Name) != string(p.text) {
			p.text = []byte(printableName(candidate.Name))
			p.selected, p.completed = candidate.ID, true
			return "", false
		}
		next := 0
		if ok {
			for i, session := range sessions {
				if session.ID == candidate.ID {
					next = (i + 1) % len(sessions)
					break
				}
			}
		}
		p.text = []byte(printableName(sessions[next].Name))
		p.selected, p.completed = sessions[next].ID, true
		return "", false
	}
	p.selected, p.completed = "", false
	return p.RenamePrompt.Feed(b)
}
