package mvu

import (
	"bytes"
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"
)

// SearchState is the local session search overlay.
type SearchState struct {
	Visible, Important bool
	Text, Ghost        string
	Label, Hint        string
	CandidateID        string
}

// SearchAction updates the local search field independently of connection status.
type SearchAction struct{ Search SearchState }

func (a SearchAction) run(r *Runtime, _ time.Time) ActionResult {
	r.mutateState(func(s *State, _ time.Time) { s.Search = a.Search })
	return ActionResult{Changed: true}
}

func searchCursor(search SearchState, cols, rows int, cursor Cursor) Cursor {
	if !search.Visible || cols < 1 || rows < 1 {
		return cursor
	}
	col := 1 + runewidth.StringWidth(searchPrefix(search)+search.Text)
	if col > cols {
		col = cols
	}
	return Cursor{Row: rows, Col: col, Visible: true}
}

func searchPrefix(search SearchState) string {
	if search.Label != "" {
		return search.Label
	}
	prefix := "/ "
	if search.Important {
		prefix = "i "
	}
	return prefix
}

func searchText(search SearchState, cols int) string {
	line := searchPrefix(search) + search.Text
	// Keep the insertion point and the tail of long names in view.
	if width := runewidth.StringWidth(line); width >= cols {
		line = runewidth.TruncateLeft(line, width-cols+1, "")
	}
	return line
}

func drawSearch(buf *bytes.Buffer, cols, rows int, search SearchState) {
	if !search.Visible || cols < 1 || rows < 1 {
		return
	}
	fmt.Fprintf(buf, "\x1b[%d;1H\x1b[0m\x1b[2K", rows)
	text := searchText(search, cols)
	buf.WriteString(text)
	remaining := cols - runewidth.StringWidth(text)
	if remaining > 0 {
		buf.WriteString("\x1b[90m")
		suffix := search.Ghost
		if search.Hint != "" {
			suffix += " (" + search.Hint + ")"
		}
		buf.WriteString(runewidth.Truncate(suffix, remaining, ""))
	}
	buf.WriteString("\x1b[0m")
}
