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
	// Excluded omits a tab from suggestions and selection while preserving its
	// position as a cycling origin in the full tab order.
	Excluded bool
}

// SearchView is an atomic snapshot of the search field and highlighted session.
type SearchView struct {
	Active, Important        bool
	Text, Ghost, CandidateID string
}

// SearchResult describes a consumed key, a changed field, or an accepted session.
type SearchResult struct {
	Consumed, Changed bool
	SessionID         string
	SessionIDs        []string
}

// SearchPrompt edits a prefix and selects sessions without changing the active tab.
// Its clock-driven Escape disambiguation is safe alongside input and rendering.
type SearchPrompt struct {
	mu           sync.Mutex
	editor       RenamePrompt
	selected     string
	origin       string
	completed    bool
	escapePrefix int
	escapeTimer  *clock.Timer
	generation   uint64
	onChange     func()
	important    *importantSearch
}

// Begin starts a fresh search. onChange redraws after a standalone Escape expires;
// ordinary input reports changes through Feed instead. A nil clock uses real time.
// currentID anchors cycling when the field is empty without previewing that tab.
func (p *SearchPrompt) Begin(clk clock.Clock, onChange func(), currentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopEscape()
	p.important = nil
	p.editor.Begin("", clk)
	p.selected, p.completed = "", false
	p.origin = currentID
	p.onChange = onChange
}

// Close stops pending Escape handling when the input owner exits.
func (p *SearchPrompt) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopEscape()
	p.editor = RenamePrompt{}
	p.important = nil
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
	if p.important != nil {
		text = importantToken(text)
	}
	for _, session := range sessions {
		if !session.Excluded && session.ID == p.selected && strings.HasPrefix(printableName(session.Name), text) {
			return session, true
		}
	}
	if text == "" || !utf8.Valid(p.editor.text) {
		return SearchSession{}, false
	}
	// An exact name takes precedence over a longer name with the same prefix.
	for _, session := range sessions {
		if !session.Excluded && printableName(session.Name) == text {
			return session, true
		}
	}
	for _, session := range sessions {
		if !session.Excluded && strings.HasPrefix(printableName(session.Name), text) {
			return session, true
		}
	}
	return SearchSession{}, false
}

// View returns entered text, the gray completion suffix, and the previewed ID.
func (p *SearchPrompt) View(sessions []SearchSession) SearchView {
	p.mu.Lock()
	defer p.mu.Unlock()
	view := SearchView{Active: p.editor.Active, Important: p.important != nil, Text: printableName(string(p.editor.text))}
	if view.Active {
		if session, ok := p.candidate(sessions); ok {
			p.selected = session.ID
			text := view.Text
			if p.important != nil {
				text = importantToken(text)
			}
			view.Ghost = strings.TrimPrefix(printableName(session.Name), text)
			view.CandidateID = session.ID
		} else if p.important != nil && importantToken(string(p.editor.text)) == "" {
			// Preview the last committed entry while the next token is empty.
			for offset, entry := range p.important.entries {
				if offset+len(entry.Name)+1 != len(p.editor.text) {
					continue
				}
				for _, session := range sessions {
					if !session.Excluded && session.ID == entry.ID && session.Name == entry.Name {
						view.CandidateID = entry.ID
						break
					}
				}
			}
		}
	}
	return view
}

func (p *SearchPrompt) cycle(sessions []SearchSession, direction int) {
	if p.important != nil {
		p.cycleImportant(sessions, direction)
		return
	}
	if len(sessions) == 0 {
		return
	}
	candidate, ok := p.candidate(sessions)
	if ok && !p.completed && printableName(candidate.Name) != string(p.editor.text) {
		p.editor.text = []byte(printableName(candidate.Name))
		p.selected, p.completed = candidate.ID, true
		return
	}
	origin := -1
	if direction < 0 {
		origin = 0
	}
	if ok {
		for i, session := range sessions {
			if session.ID == candidate.ID {
				origin = i
				break
			}
		}
	} else if len(p.editor.text) == 0 {
		for i, session := range sessions {
			if session.ID == p.origin {
				origin = i
				break
			}
		}
	}
	for step := 1; step <= len(sessions); step++ {
		next := (origin + direction*step + len(sessions)*2) % len(sessions)
		if !sessions[next].Excluded {
			p.editor.text = []byte(printableName(sessions[next].Name))
			p.selected, p.completed = sessions[next].ID, true
			return
		}
	}
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
	if p.important != nil {
		return p.feedImportant(b, sessions)
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
