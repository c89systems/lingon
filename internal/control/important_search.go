package control

import (
	"strings"
	"unicode/utf8"

	"pkt.systems/lingon/internal/clock"
)

// importantSearch retains identities only for the prefilled and Tab-completed
// tokens. Manually entered tokens must resolve unambiguously when accepted.
// All access is protected by the enclosing SearchPrompt's mutex.
type importantSearch struct {
	entries map[int]SearchSession
}

// BeginImportant edits an atomic batch of importance toggles, initially containing
// the current parent tab followed by a space. It shares search's input handling,
// completion preview and clock-driven cancellation, without switching tabs.
func (p *SearchPrompt) BeginImportant(clk clock.Clock, onChange func(), currentID string, sessions []SearchSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopEscape()
	p.editor.Begin("", clk)
	p.selected, p.completed = "", false
	p.origin, p.onChange = currentID, onChange
	p.important = &importantSearch{entries: make(map[int]SearchSession)}
	for _, session := range sessions {
		if session.ID == currentID {
			p.editor.text = []byte(session.Name + " ")
			p.important.entries[0] = session
			break
		}
	}
}

func importantToken(text string) string {
	return text[strings.LastIndexByte(text, ' ')+1:]
}

func (p *SearchPrompt) cycleImportant(sessions []SearchSession, direction int) {
	text := string(p.editor.text)
	start := len(text) - len(importantToken(text))
	candidate, found := p.candidate(sessions)
	if !found && start == len(text) && len(sessions) != 0 {
		origin := p.origin
		used := make(map[string]bool)
		for offset, entry := range p.important.entries {
			used[entry.ID] = true
			if offset+len(entry.Name)+1 == start {
				origin = entry.ID
			}
		}
		if ids, ok := p.acceptImportant(sessions); ok && len(ids) > 0 {
			for _, id := range ids {
				used[id] = true
			}
			origin = ids[len(ids)-1]
		}
		index := -1
		if direction < 0 {
			index = 0
		}
		for i, session := range sessions {
			if session.ID == origin {
				index = i
				break
			}
		}
		for step := 1; step <= len(sessions); step++ {
			session := sessions[(index+direction*step+2*len(sessions))%len(sessions)]
			if !session.Excluded && !used[session.ID] {
				candidate, found = session, true
				break
			}
		}
	}
	if !found {
		return
	}
	p.editor.text = []byte(text[:start] + candidate.Name + " ")
	p.important.entries[start] = candidate
	p.origin = candidate.ID
	p.selected = ""
}

func (p *SearchPrompt) acceptImportant(sessions []SearchSession) ([]string, bool) {
	ids := make([]string, 0)
	seen := make(map[string]bool)
	text := string(p.editor.text)
	for start := 0; start < len(text); {
		if text[start] == ' ' {
			start++
			continue
		}
		end := start + len(importantTokenUntilSpace(text[start:]))
		token := text[start:end]
		if !utf8.ValidString(token) {
			return nil, false
		}
		id := ""
		if pinned, ok := p.important.entries[start]; ok && pinned.Name == token {
			for _, session := range sessions {
				if !session.Excluded && session.ID == pinned.ID && session.Name == token {
					id = session.ID
					break
				}
			}
			if id == "" {
				return nil, false
			}
		} else {
			// Exact names win over prefixes, but duplicate exact names are ambiguous.
			matches, exacts := 0, 0
			for _, session := range sessions {
				if session.Excluded {
					continue
				}
				if session.Name == token {
					exacts++
					id = session.ID
				}
			}
			if exacts > 1 {
				return nil, false
			}
			if exacts == 0 {
				for _, session := range sessions {
					if !session.Excluded && strings.HasPrefix(session.Name, token) {
						matches++
						id = session.ID
					}
				}
				if matches != 1 {
					return nil, false
				}
			}
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		start = end
	}
	return ids, true
}

func importantTokenUntilSpace(text string) string {
	if i := strings.IndexByte(text, ' '); i >= 0 {
		return text[:i]
	}
	return text
}

func (p *SearchPrompt) feedImportant(b byte, sessions []SearchSession) SearchResult {
	result := SearchResult{Consumed: true, Changed: true}
	if b == '\r' || b == '\n' {
		if ids, ok := p.acceptImportant(sessions); ok {
			p.editor.Active = false
			result.SessionIDs = ids
		}
		return result
	}
	if b == '\t' {
		p.cycleImportant(sessions, 1)
		return result
	}
	p.selected = ""
	if b == 0x17 { // Ctrl+W: separators are spaces, never punctuation.
		text := strings.TrimRight(string(p.editor.text), " ")
		p.editor.text = []byte(text[:strings.LastIndexByte(text, ' ')+1])
	} else if b >= 0x20 && b != 0x7f {
		if len(p.editor.text) < 16384 {
			p.editor.text = append(p.editor.text, b)
		}
	} else {
		p.editor.Feed(b)
	}
	for offset, entry := range p.important.entries {
		end := offset + len(entry.Name)
		if end > len(p.editor.text) || string(p.editor.text[offset:end]) != entry.Name || (end < len(p.editor.text) && p.editor.text[end] != ' ') {
			delete(p.important.entries, offset)
		}
	}
	return result
}
