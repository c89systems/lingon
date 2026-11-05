package mvu

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// ScrollbackSource identifies the retained history and live rows for an update.
type ScrollbackSource struct {
	Position                        ScrollbackPosition
	HistoryRows, LiveRows, ViewRows int
}

type scrollbackCopy struct {
	visual, selecting bool
	cursor, anchor    int
	base, total, rows int
}

// ScrollbackLineRange identifies inclusive rows in the current retained source.
type ScrollbackLineRange struct{ First, Last int }

// SyncSource updates history anchoring and the bounds of the visual cursor.
func (v *ScrollbackViewport) SyncSource(source ScrollbackSource) {
	if v == nil {
		return
	}
	v.SyncHistory(source.Position)
	if source.Position != v.history {
		return
	}
	if v.copy.visual && v.copy.rows > 0 {
		v.offset += v.copy.rows - source.ViewRows
	}
	v.copy.base = source.Position.End - source.HistoryRows
	v.copy.total = source.HistoryRows + source.LiveRows
	v.copy.rows = source.ViewRows
	if v.copy.visual {
		last := v.copy.base + max(0, v.copy.total-1)
		v.copy.cursor = min(max(v.copy.cursor, v.copy.base), last)
		v.copy.anchor = min(max(v.copy.anchor, v.copy.base), last)
		v.keepCopyCursorVisible()
	}
}

// Visual reports whether scrollback's vertical copy cursor is active.
func (v *ScrollbackViewport) Visual() bool { return v != nil && v.copy.visual }

// CopyKey handles visual-copy input. Space starts or commits a selection;
// Enter commits only an existing selection and leaves the visual cursor active.
func (v *ScrollbackViewport) CopyKey(key byte) (ScrollbackLineRange, bool) {
	switch key {
	case 'v':
		v.ToggleVisual()
	case ' ':
		return v.SelectLines()
	case '\r', '\n':
		if v.Visual() && v.copy.selecting {
			return v.SelectLines()
		}
	}
	return ScrollbackLineRange{}, false
}

// ToggleVisual enables a middle-left cursor, or cancels it and any selection.
func (v *ScrollbackViewport) ToggleVisual() {
	if v == nil || !v.active {
		return
	}
	if v.copy.visual {
		v.copy.visual, v.copy.selecting = false, false
		return
	}
	v.col = 0
	v.copy.visual = true
	start := max(0, v.copy.total-v.copy.rows-v.offset)
	v.copy.cursor = v.copy.base + min(max(0, v.copy.total-1), start+max(0, (v.copy.rows-1)/2))
	v.keepCopyCursorVisible()
}

// SelectLines starts selection, or ends it and returns the inclusive copy range.
func (v *ScrollbackViewport) SelectLines() (ScrollbackLineRange, bool) {
	if !v.Visual() {
		return ScrollbackLineRange{}, false
	}
	if !v.copy.selecting {
		v.copy.anchor, v.copy.selecting = v.copy.cursor, true
		return ScrollbackLineRange{}, false
	}
	v.copy.selecting = false
	return ScrollbackLineRange{First: min(v.copy.anchor, v.copy.cursor) - v.copy.base, Last: max(v.copy.anchor, v.copy.cursor) - v.copy.base}, true
}

func (v *ScrollbackViewport) moveCopyCursor(delta int) bool {
	before, offset := v.copy.cursor, v.offset
	v.copy.cursor = min(max(v.copy.cursor+delta, v.copy.base), v.copy.base+max(0, v.copy.total-1))
	v.keepCopyCursorVisible()
	return before != v.copy.cursor || offset != v.offset
}

func (v *ScrollbackViewport) keepCopyCursorVisible() {
	start := max(0, v.copy.total-v.copy.rows-v.offset)
	row := v.copy.cursor - v.copy.base
	if row < start {
		start = row
	}
	if row >= start+v.copy.rows {
		start = row - max(1, v.copy.rows) + 1
	}
	v.SetOffset(v.copy.total, v.copy.rows, v.copy.total-v.copy.rows-start)
}

// DecorateCopy projects cursor and selection onto a detached viewport snapshot.
// It never updates the model or modifies the supplied snapshot.
func (v *ScrollbackViewport) DecorateCopy(snap *protocolpb.Snapshot, source ScrollbackSource) *protocolpb.Snapshot {
	if !v.Visual() || snap == nil || snap.Cols == 0 || snap.Rows == 0 || source.Position.Generation != v.history.Generation {
		return snap
	}
	out := proto.Clone(snap).(*protocolpb.Snapshot)
	if len(out.Modes) < int(out.Cols*out.Rows) {
		modes := make([]int32, int(out.Cols*out.Rows))
		copy(modes, out.Modes)
		out.Modes = modes
	}
	base := source.Position.End - source.HistoryRows
	total := source.HistoryRows + source.LiveRows
	start := max(0, total-int(out.Rows)-v.OffsetFor(source.Position))
	row := min(max(v.copy.cursor-base, 0), max(0, total-1)) - start
	out.Cursor = &protocolpb.Cursor{Y: uint32(min(max(row, 0), max(0, int(out.Rows)-1)))}
	out.CursorVisible = true
	if v.copy.selecting {
		first, last := min(v.copy.cursor, v.copy.anchor), max(v.copy.cursor, v.copy.anchor)
		for y := 0; y < int(out.Rows); y++ {
			absolute := base + start + y
			if absolute < first || absolute > last {
				continue
			}
			for x := 0; x < int(out.Cols); x++ {
				out.Modes[y*int(out.Cols)+x] ^= int32(terminal.ModeInverse)
			}
		}
	}
	return out
}

// ScrollbackTextSource supplies full, unpanned rows for line-wise copying.
type ScrollbackTextSource interface {
	Rows() int
	Text(row int) string
}

type terminalTextSource struct {
	history []terminal.ScrollbackRow
	live    *protocolpb.Snapshot
}
type protoTextSource struct {
	history []*protocolpb.ScrollbackRow
	live    *protocolpb.Snapshot
}

// NewTerminalTextSource adapts host history to full-line copying.
func NewTerminalTextSource(history []terminal.ScrollbackRow, live *protocolpb.Snapshot) ScrollbackTextSource {
	return terminalTextSource{history: history, live: live}
}

// NewProtoTextSource adapts attach history to full-line copying.
func NewProtoTextSource(history []*protocolpb.ScrollbackRow, live *protocolpb.Snapshot) ScrollbackTextSource {
	return protoTextSource{history: history, live: live}
}
func (s terminalTextSource) Rows() int { return len(s.history) + int(s.live.GetRows()) }
func (s protoTextSource) Rows() int    { return len(s.history) + int(s.live.GetRows()) }
func (s terminalTextSource) Text(row int) string {
	if row < 0 || row >= s.Rows() {
		return ""
	}
	if row < len(s.history) {
		cells := s.history[row].Cells
		return copyRowText(len(cells), func(x int) (rune, string) { return cells[x].Rune, cells[x].Grapheme })
	}
	return snapshotRowText(s.live, row-len(s.history))
}
func (s protoTextSource) Text(row int) string {
	if row < 0 || row >= s.Rows() {
		return ""
	}
	if row < len(s.history) {
		r := s.history[row]
		return copyRowText(len(r.GetRunes()), func(x int) (rune, string) {
			g := ""
			if x < len(r.Graphemes) {
				g = r.Graphemes[x]
			}
			return rune(r.Runes[x]), g
		})
	}
	return snapshotRowText(s.live, row-len(s.history))
}
func snapshotRowText(s *protocolpb.Snapshot, row int) string {
	return copyRowText(int(s.GetCols()), func(x int) (rune, string) {
		i := row*int(s.Cols) + x
		r := rune(0)
		g := ""
		if i < len(s.Runes) {
			r = rune(s.Runes[i])
		}
		if i < len(s.Graphemes) {
			g = s.Graphemes[i]
		}
		return r, g
	})
}
func copyRowText(cols int, cell func(int) (rune, string)) string {
	var out strings.Builder
	skip := 0
	for x := 0; x < cols; x++ {
		r, g := cell(x)
		if skip > 0 && r == 0 && g == "" {
			skip--
			continue
		}
		skip = 0
		if g != "" {
			g = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return ' '
				}
				return r
			}, g)
			out.WriteString(g)
			skip = max(0, runewidth.StringWidth(g)-1)
		} else if r == 0 || unicode.IsControl(r) {
			out.WriteByte(' ')
		} else {
			out.WriteRune(r)
			skip = max(0, runewidth.RuneWidth(r)-1)
		}
	}
	return strings.TrimRight(out.String(), " ")
}

// CopyScrollbackLines copies full rows, trimming display padding and ending each
// physical terminal row with a newline, including blank rows.
func CopyScrollbackLines(source ScrollbackTextSource, rows ScrollbackLineRange) string {
	var out strings.Builder
	for row := max(0, rows.First); row <= min(rows.Last, source.Rows()-1); row++ {
		out.WriteString(source.Text(row))
		out.WriteByte('\n')
	}
	return out.String()
}
