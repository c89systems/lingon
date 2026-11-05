package session

import (
	"context"
	"os"
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
	actualCols, actualRows := termSizeAny(r.stdout(), r.stdin())
	r.geometryMu.Lock()
	r.windowObserved = terminal.WindowSize{Cols: actualCols, Rows: actualRows}
	r.geometryMu.Unlock()
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

// pendingWindowSize gives explicit host sizing precedence over restore. Restore
// targets come from the session, so disabling restore leaves no forced geometry
// behind on hidden local tabs.
func (r *Runner) pendingWindowSize(id string) terminal.WindowSize {
	if request := r.windowRequest(id); request != nil {
		if size := request.Pending(); size.Cols > 0 && size.Rows > 0 {
			return size
		}
	}
	if r.restoreEnabled() {
		if local := r.localSession(id); local != nil {
			cols, rows := local.Size()
			return terminal.WindowSize{Cols: cols, Rows: rows}
		}
	}
	return terminal.WindowSize{}
}

func (r *Runner) requestPendingWindow(ctx context.Context, id string) {
	request := r.windowRequest(id)
	size := r.pendingWindowSize(id)
	if size.Cols <= 0 || size.Rows <= 0 {
		return
	}
	cols, rows := termSizeAny(r.stdout(), r.stdin())
	if request != nil {
		request.Observe(terminal.WindowSize{Cols: cols, Rows: rows})
	}
	pending := terminal.WindowSize{}
	if r.windowRestore != nil {
		pending = r.windowRestore.Latest()
	}
	if cols == size.Cols && rows == size.Rows && (pending == (terminal.WindowSize{}) || pending == size) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.stdoutMu.Lock()
	if r.windowRestore != nil {
		r.windowRestore.Requested(size)
	}
	err := writeAll(ctx, r.stdout(), terminal.WindowResizeSequence(size), r.clock)
	r.stdoutMu.Unlock()
	if err != nil || r.effects == nil {
		return
	}
	r.effects.Schedule(ctx, "window-size", time.Second, func() {
		active, _ := r.activeSession()
		if active != id || r.pendingWindowSize(id) != size {
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
	actual := terminal.WindowSize{Cols: cols, Rows: rows}
	r.geometryMu.Lock()
	previous := r.windowObserved
	r.windowObserved = actual
	r.geometryMu.Unlock()
	if !activeLocal && r.remoteSessions != nil && (r.restoreEnabled() || (r.windowRestore != nil && r.windowRestore.Latest() != (terminal.WindowSize{}))) {
		if actual != previous {
			r.setTerminalGeometry(cols, rows)
			r.resizePopup()
			_ = r.remoteSessions.processTerminalResize(r.runCtx, activeID)
			return true
		}
		return false
	}
	// Input reads also check dimensions. An unchanged window cannot acknowledge
	// a new request: a previous tab's older resize may still arrive afterwards.
	if actual != previous && r.windowRestore != nil && r.windowRestore.Acknowledge(actual) {
		if request := r.windowRequest(activeID); request != nil {
			request.Observe(actual)
		}
		return false
	}
	if r.restoreEnabled() {
		if actual == previous {
			return false
		}
		if request := r.windowRequest(activeID); request != nil {
			request.Set(terminal.WindowSize{})
		}
	}
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

func (r *Runner) restoreEnabled() bool { return r.windowRestore != nil && r.windowRestore.Load() }

func (r *Runner) toggleRestore(ctx context.Context, stdout *os.File) {
	id, local := r.activeSession()
	enabled := false
	if !local && r.remoteSessions != nil {
		enabled = r.remoteSessions.toggleRestore(id)
	} else {
		enabled = !r.restoreEnabled()
		r.windowRestore.Store(enabled)
		if enabled {
			if session := r.localSession(id); session != nil {
				cols, rows := session.Size()
				r.setTerminalGeometry(cols, rows)
				r.requestPendingWindow(ctx, id)
			}
		}
	}
	if enabled {
		r.showStatus("Restore: follow session size", stdout, 2*time.Second)
	} else {
		r.showStatus("Restore off", stdout, 2*time.Second)
		cols, rows := termSizeAny(stdout, r.stdin())
		r.ResizeActive(cols, rows)
	}
	if !local && r.remoteSessions != nil {
		r.remoteSessions.Render(id)
	}
}
