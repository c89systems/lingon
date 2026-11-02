package control

import (
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"pkt.systems/lingon/internal/clock"
)

const searchEscapeDelay = 50 * time.Millisecond

// SearchSession identifies one searchable tab. Names need not be unique.
type SearchSession struct {
	ID, Name string
}

// SearchView is an atomic snapshot of the search field and highlighted session.
type SearchView struct {
	Active                   bool
	Text, Ghost, CandidateID string
}

// SearchResult describes a consumed key, a changed field, or an accepted session.
type SearchResult struct {
	Consumed, Changed bool
	SessionID         string
}

// SearchPrompt edits a prefix and selects sessions without changing the active tab.
// Its clock-driven Escape disambiguation is safe alongside input and rendering.
type SearchPrompt struct {
	mu           sync.Mutex
	editor       RenamePrompt
	selected     string
	completed    bool
	escapePrefix int
	escapeTimer  *clock.Timer
	generation   uint64
	onChange     func()
}

// Begin starts a fresh search. onChange redraws after a standalone Escape expires;
// ordinary input reports changes through Feed instead. A nil clock uses real time.
func (p *SearchPrompt) Begin(clk clock.Clock, onChange func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopEscape()
	p.editor.Begin("", clk)
	p.selected, p.completed = "", false
	p.onChange = onChange
}

// Close stops pending Escape handling when the input owner exits.
func (p *SearchPrompt) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopEscape()
	p.editor = RenamePrompt{}
	p.onChange = nil
}

// HandlesInput reports whether editing or an escape tail needs input.
func (p *SearchPrompt) HandlesInput() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.editor.Active || p.editor.escape
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
	text := string(p.editor.text)
	for _, session := range sessions {
		if session.ID == p.selected && strings.HasPrefix(printableName(session.Name), text) {
			return session, true
		}
	}
	if text == "" || !utf8.Valid(p.editor.text) {
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

// View returns entered text, the gray completion suffix, and the previewed ID.
func (p *SearchPrompt) View(sessions []SearchSession) SearchView {
	p.mu.Lock()
	defer p.mu.Unlock()
	view := SearchView{Active: p.editor.Active, Text: printableName(string(p.editor.text))}
	if view.Active {
		if session, ok := p.candidate(sessions); ok {
			p.selected = session.ID
			view.Ghost = strings.TrimPrefix(printableName(session.Name), view.Text)
			view.CandidateID = session.ID
		}
	}
	return view
}

func (p *SearchPrompt) cycle(sessions []SearchSession, direction int) {
	if len(sessions) == 0 {
		return
	}
	candidate, ok := p.candidate(sessions)
	if ok && !p.completed && printableName(candidate.Name) != string(p.editor.text) {
		p.editor.text = []byte(printableName(candidate.Name))
		p.selected, p.completed = candidate.ID, true
		return
	}
	next := 0
	if direction < 0 {
		next = len(sessions) - 1
	}
	if ok {
		for i, session := range sessions {
			if session.ID == candidate.ID {
				next = (i + direction + len(sessions)) % len(sessions)
				break
			}
		}
	}
	p.editor.text = []byte(printableName(sessions[next].Name))
	p.selected, p.completed = sessions[next].ID, true
}

func (p *SearchPrompt) stopEscape() {
	p.escapeTimer.Stop()
	p.escapeTimer = nil
	p.escapePrefix = 0
	p.generation++
}

func (p *SearchPrompt) cancelEscape() {
	prefix := p.escapePrefix
	p.stopEscape()
	p.editor.Feed(0x1b)
	if prefix == 2 {
		p.editor.DiscardEscapeTail('[')
	}
}

func (p *SearchPrompt) startEscape() {
	p.escapePrefix = 1
	p.generation++
	generation := p.generation
	p.escapeTimer = p.editor.clock.AfterFunc(searchEscapeDelay, func() {
		p.mu.Lock()
		if p.generation != generation || p.escapePrefix == 0 {
			p.mu.Unlock()
			return
		}
		p.cancelEscape()
		onChange := p.onChange
		p.mu.Unlock()
		if onChange != nil {
			onChange()
		}
	})
}

// Feed recognizes streaming Shift+Tab (CSI Z), editing keys, and cancellation.
// A plain key following Escape is left for the application; escape tails are
// consumed. Enter selects an immutable ID; a missing match keeps the field open.
func (p *SearchPrompt) Feed(b byte, sessions []SearchSession) SearchResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.editor.Active {
		return SearchResult{Consumed: p.editor.DiscardEscapeTail(b)}
	}
	if p.escapePrefix != 0 {
		if p.escapePrefix == 1 && b == '[' {
			p.escapePrefix = 2
			return SearchResult{Consumed: true}
		}
		if p.escapePrefix == 2 && b == 'Z' {
			p.stopEscape()
			p.cycle(sessions, -1)
			return SearchResult{Consumed: true, Changed: true}
		}
		p.cancelEscape()
		return SearchResult{Consumed: p.editor.DiscardEscapeTail(b), Changed: true}
	}
	if b == 0x1b {
		p.startEscape()
		return SearchResult{Consumed: true}
	}
	result := SearchResult{Consumed: true, Changed: true}
	if b == '\r' || b == '\n' {
		if session, ok := p.candidate(sessions); ok {
			p.editor.Active = false
			result.SessionID = session.ID
		}
		return result
	}
	if b == '\t' {
		p.cycle(sessions, 1)
		return result
	}
	p.selected, p.completed = "", false
	p.editor.Feed(b)
	return result
}
