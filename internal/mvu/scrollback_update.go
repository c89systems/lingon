package mvu

import (
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
)

// ScrollbackSource projects an immutable history snapshot into a viewport.
type ScrollbackSource interface {
	length() int
	width(*protocolpb.Snapshot) int
	project(scrollbackProjection) *protocolpb.Snapshot
}

type scrollbackProjection struct {
	live                       *protocolpb.Snapshot
	cols, rows, offset, column int
}
type terminalScrollbackSource struct{ rows []terminal.ScrollbackRow }
type protoScrollbackSource struct{ rows []*protocolpb.ScrollbackRow }

// NewTerminalScrollbackSource wraps immutable terminal history rows.
func NewTerminalScrollbackSource(rows []terminal.ScrollbackRow) ScrollbackSource {
	return terminalScrollbackSource{rows}
}

// NewProtoScrollbackSource wraps attach-owned immutable history rows.
func NewProtoScrollbackSource(rows []*protocolpb.ScrollbackRow) ScrollbackSource {
	return protoScrollbackSource{rows}
}
func (s terminalScrollbackSource) length() int { return len(s.rows) }
func (s terminalScrollbackSource) width(live *protocolpb.Snapshot) int {
	return scrollbackTerminalWidth(s.rows, live)
}
func (s terminalScrollbackSource) project(p scrollbackProjection) *protocolpb.Snapshot {
	return BuildScrollbackViewFromTerminal(p.cols, p.rows, s.rows, p.live, p.offset, p.column)
}
func (s protoScrollbackSource) length() int { return len(s.rows) }
func (s protoScrollbackSource) width(live *protocolpb.Snapshot) int {
	return scrollbackProtoWidth(s.rows, live)
}
func (s protoScrollbackSource) project(p scrollbackProjection) *protocolpb.Snapshot {
	return BuildScrollbackViewFromProto(p.cols, p.rows, s.rows, p.live, p.offset, p.column)
}

// ScrollbackContent is one model input; neither the live snapshot nor history
// rows may be mutated after Update. LivePosition records the live grid origin.
type ScrollbackContent struct {
	SessionID    string
	Live         *protocolpb.Snapshot
	History      ScrollbackSource
	Position     ScrollbackPosition
	LivePosition ScrollbackPosition
	Cols, Rows   int
}

// ScrollbackOperation identifies viewport events handled by Update.
type ScrollbackOperation uint8

const (
	// ScrollbackIngest ingests data without moving the user's anchor.
	ScrollbackIngest ScrollbackOperation = iota
	// ScrollbackEnter enters at the currently visible live viewport.
	ScrollbackEnter
	// ScrollbackExit resumes the live screen.
	ScrollbackExit
	// ScrollbackPage moves by Delta*Step rows.
	ScrollbackPage
	// ScrollbackPan moves horizontally by Delta columns.
	ScrollbackPan
	// ScrollbackTop selects the oldest retained rows.
	ScrollbackTop
	// ScrollbackBottom selects the current live bottom without exiting.
	ScrollbackBottom
)

// ScrollbackUpdate is an input message, reduced before View or terminal output.
type ScrollbackUpdate struct {
	Content     *ScrollbackContent
	Operation   ScrollbackOperation
	Delta, Step int
}

// ScrollbackFrame is a pure projection of the reduced viewport model.
type ScrollbackFrame struct {
	Snapshot        *protocolpb.Snapshot
	Active, Visible bool
	Percent         int
}

// Update is the only production mutation boundary for the viewport model.
// History advances first, navigation follows, and View never corrects state.
func (v *ScrollbackViewport) Update(msg ScrollbackUpdate) bool {
	if v == nil {
		return false
	}
	if data := msg.Content; data != nil {
		if v.content.SessionID != "" && data.SessionID != v.content.SessionID {
			*v = ScrollbackViewport{}
		}
		stale := v.historyKnown && (data.Position.Generation < v.history.Generation || (data.Position.Generation == v.history.Generation && data.Position.End < v.history.End))
		if !stale {
			v.content = *data
			if v.content.Live == nil {
				v.content.Live = BlankSnapshot(max(1, data.Cols), max(1, data.Rows))
			}
			if v.content.History == nil {
				v.content.History = NewProtoScrollbackSource(nil)
			}
			if v.content.Cols <= 0 {
				v.content.Cols = int(v.content.Live.Cols)
			}
			if v.content.Rows <= 0 {
				v.content.Rows = int(v.content.Live.Rows)
			}
			v.SyncHistory(data.Position)
			v.Normalize(v.totalRows(), v.content.Rows, v.content.History.width(v.content.Live), v.content.Cols)
		}
	}
	if v.content.History == nil {
		v.content.History = NewProtoScrollbackSource(nil)
	}
	if v.content.Live == nil {
		v.content.Live = BlankSnapshot(1, 1)
	}
	if v.content.Cols <= 0 {
		v.content.Cols = int(v.content.Live.Cols)
	}
	if v.content.Rows <= 0 {
		v.content.Rows = int(v.content.Live.Rows)
	}
	beforeOffset, beforeCol, beforeActive := v.offset, v.col, v.active
	total, rows := v.totalRows(), v.content.Rows
	switch msg.Operation {
	case ScrollbackEnter:
		col, origin := render.ViewportOriginForSnapshot(v.content.Live, v.content.Cols, rows)
		v.EnterAt(total, rows, max(0, int(v.content.Live.GetRows())-rows-origin), v.content.History.width(v.content.Live), v.content.Cols, col)
		v.SyncHistory(v.content.Position)
	case ScrollbackExit:
		v.Exit()
	case ScrollbackPage:
		v.Page(total, rows, msg.Delta, msg.Step)
	case ScrollbackPan:
		v.PanX(v.content.History.width(v.content.Live), v.content.Cols, msg.Delta)
	case ScrollbackTop:
		v.Top(total, rows)
	case ScrollbackBottom:
		v.Bottom()
	}
	return beforeOffset != v.offset || beforeCol != v.col || beforeActive != v.active
}

func (v *ScrollbackViewport) totalRows() int {
	total := int(v.content.Live.GetRows())
	if v.content.History != nil {
		total += v.content.History.length()
	}
	return total
}

// View reads the model. Separate history and live transport messages retain
// their row coordinates until the corresponding live frame arrives.
func (v *ScrollbackViewport) View() ScrollbackFrame {
	if v == nil {
		return ScrollbackFrame{}
	}
	frame := ScrollbackFrame{Snapshot: v.content.Live, Active: v.Active(), Visible: v.Visible(), Percent: v.Percent(v.totalRows(), v.content.Rows)}
	if frame.Visible && v.content.History != nil {
		live := v.content.Live
		if advanced := v.content.Position.End - v.content.LivePosition.End; advanced > 0 && v.content.Position.Generation == v.content.LivePosition.Generation {
			live = alignPendingLiveRows(live, advanced)
		}
		frame.Snapshot = v.content.History.project(scrollbackProjection{live: live, cols: v.content.Cols, rows: v.content.Rows, offset: v.Offset(), column: v.Column()})
	}
	return frame
}

// A history-only message moves rows out of the previous live grid. Retain their
// absolute coordinates rather than briefly drawing that grid a second time.
func alignPendingLiveRows(live *protocolpb.Snapshot, advanced int) *protocolpb.Snapshot {
	if live == nil || advanced <= 0 {
		return live
	}
	out := protocolpb.Snapshot{Cols: live.Cols, Rows: live.Rows, Cursor: live.Cursor, CursorVisible: live.CursorVisible, Mode: live.Mode, Title: live.Title}
	cols, rows := int(live.Cols), int(live.Rows)
	out.Runes = shiftLiveRows(live.Runes, cols, rows, advanced)
	out.Modes = shiftLiveRows(live.Modes, cols, rows, advanced)
	out.Fg = shiftLiveRows(live.Fg, cols, rows, advanced)
	out.Bg = shiftLiveRows(live.Bg, cols, rows, advanced)
	out.Graphemes = shiftLiveRows(live.Graphemes, cols, rows, advanced)
	return &out
}

func shiftLiveRows[T any](values []T, cols, rows, advanced int) []T {
	if len(values) == 0 {
		return nil
	}
	next := append([]T(nil), values...)
	if cols <= 0 {
		return next
	}
	completeRows := min(rows, len(values)/cols)
	for y := 0; y+advanced < completeRows; y++ {
		copy(next[y*cols:(y+1)*cols], values[(y+advanced)*cols:(y+advanced+1)*cols])
	}
	return next
}
