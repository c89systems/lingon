package session

import (
	"context"
	"strings"
	"time"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/terminal"
)

// terminalGeometry snapshots the pair shared by resize and terminal creation.
// Never hold geometryMu during terminal, process, render or network I/O.
func (r *Runner) terminalGeometry() (int, int) {
	r.geometryMu.RLock()
	defer r.geometryMu.RUnlock()
	return r.opts.Cols, r.opts.Rows
}

func (r *Runner) setTerminalGeometry(cols, rows int) {
	r.geometryMu.Lock()
	r.opts.Cols, r.opts.Rows = cols, rows
	r.geometryMu.Unlock()
}

func (r *Runner) initializeTerminalGeometry() {
	cols, rows := r.terminalGeometry()
	explicit := cols > 0 || rows > 0
	if cols <= 0 || rows <= 0 {
		detectedCols, detectedRows := termSizeAny(r.stdout(), r.stdin())
		if detectedCols > 0 && detectedRows > 0 {
			if cols <= 0 {
				cols = detectedCols
			}
			if rows <= 0 {
				rows = detectedRows
			}
		}
	}
	if cols <= 0 {
		cols = config.DefaultTerminalCols
	}
	if rows <= 0 {
		rows = config.DefaultTerminalRows
	}
	r.setTerminalGeometry(cols, rows)
	if explicit && terminal.IsTerminal(r.stdout()) {
		r.storeWindowRequest(r.opts.SessionID, terminal.WindowSize{Cols: cols, Rows: rows})
	}
}

func (r *Runner) storeWindowRequest(id string, size terminal.WindowSize) {
	r.geometryMu.Lock()
	if r.windowSizes == nil {
		r.windowSizes = make(map[string]*terminal.WindowSizeRequest)
	}
	request := r.windowSizes[id]
	if request == nil {
		request = &terminal.WindowSizeRequest{}
		r.windowSizes[id] = request
	}
	r.geometryMu.Unlock()
	request.Set(size)
}

func (r *Runner) windowRequest(id string) *terminal.WindowSizeRequest {
	r.geometryMu.RLock()
	defer r.geometryMu.RUnlock()
	return r.windowSizes[id]
}

func (r *Runner) requestPendingWindow(ctx context.Context, id string) {
	request := r.windowRequest(id)
	if request == nil {
		return
	}
	size := request.Pending()
	if size.Cols <= 0 || size.Rows <= 0 {
		return
	}
	cols, rows := termSizeAny(r.stdout(), r.stdin())
	request.Observe(terminal.WindowSize{Cols: cols, Rows: rows})
	if cols == size.Cols && rows == size.Rows {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.stdoutMu.Lock()
	err := writeAll(ctx, r.stdout(), terminal.WindowResizeSequence(size), r.clock)
	r.stdoutMu.Unlock()
	if err != nil || r.effects == nil {
		return
	}
	r.effects.Schedule(ctx, "window-size", time.Second, func() {
		active, _ := r.activeSession()
		if active != id || request.Pending() != size {
			return
		}
		cols, rows := termSizeAny(r.stdout(), r.stdin())
		if cols == size.Cols && rows == size.Rows {
			return
		}
		r.showErrorStatus("Terminal did not accept requested geometry", r.stdout(), 5*time.Second)
	})
}

// ResizeActive applies a size update to the current active session.
func (r *Runner) ResizeActive(cols, rows int) {
	if r == nil || cols <= 0 || rows <= 0 {
		return
	}
	r.setTerminalGeometry(cols, rows)
	r.resizePopup()
	activeID, activeLocal := r.activeSession()
	if activeLocal {
		targetID := strings.TrimSpace(activeID)
		if targetID == "" {
			targetID = r.firstLocalID()
		}
		local := r.localSession(targetID)
		if local == nil {
			return
		}
		r.resizeLocalSession(local, cols, rows)
		return
	}
	if r.remoteSessions != nil && strings.TrimSpace(activeID) != "" {
		_ = r.remoteSessions.SendResize(context.Background(), activeID, cols, rows)
	}
}

// ResizeActiveIfChanged applies a size update only when the active session has
// not already observed the requested dimensions.
func (r *Runner) ResizeActiveIfChanged(cols, rows int) bool {
	if r == nil || cols <= 0 || rows <= 0 {
		return false
	}
	activeID, activeLocal := r.activeSession()
	if activeLocal {
		if request := r.windowRequest(activeID); request != nil {
			size := request.Observe(terminal.WindowSize{Cols: cols, Rows: rows})
			cols, rows = size.Cols, size.Rows
		}
		targetID := strings.TrimSpace(activeID)
		if targetID == "" {
			targetID = r.firstLocalID()
		}
		local := r.localSession(targetID)
		if local == nil {
			return false
		}
		curCols, curRows := local.Size()
		if curCols == cols && curRows == rows {
			return false
		}
		r.ResizeActive(cols, rows)
		return true
	}
	curCols, curRows := r.terminalGeometry()
	if curCols == cols && curRows == rows {
		return false
	}
	r.ResizeActive(cols, rows)
	return true
}
