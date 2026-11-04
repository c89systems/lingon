package attach

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

type restoreRequest struct{ cols, rows, localCols, localRows int }

// A terminal window outlives its selected tab. Requests remain pending when the
// issuing client is hidden; observing a newer request also acknowledges older
// ones whose SIGWINCH events were coalesced by the terminal.
type restoreWindow struct {
	mu      sync.Mutex
	pending []terminal.WindowSize
}

func (w *restoreWindow) requested(size terminal.WindowSize) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Repeating the same target need not produce another SIGWINCH. Keep only
	// one consecutive request so it cannot later swallow a real user resize.
	if len(w.pending) > 0 && w.pending[len(w.pending)-1] == size {
		return
	}
	w.pending = append(w.pending, size)
}

func (w *restoreWindow) acknowledge(size terminal.WindowSize) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, pending := range w.pending {
		if pending == size {
			w.pending = w.pending[i+1:]
			return true
		}
	}
	return false
}

func (w *restoreWindow) latest() terminal.WindowSize {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return terminal.WindowSize{}
	}
	return w.pending[len(w.pending)-1]
}

func (c *Client) restoreEnabled() bool {
	if c.isEmbedded() {
		return false
	}
	return c.restoreState().Load()
}

// restoreState may be called by host render callbacks before Run starts. Both
// paths must initialize through the same barrier; a supplied multi-attach mode
// stays shared across clients.
func (c *Client) restoreState() *atomic.Bool {
	c.restoreOnce.Do(func() {
		if c.restoreMode == nil {
			c.restoreMode = newRestoreMode(c.Restore)
		}
		if c.restoreWindow == nil {
			c.restoreWindow = &restoreWindow{}
		}
		if c.restoreManual == nil {
			c.restoreManual = &terminal.WindowSizeRequest{}
		}
	})
	return c.restoreMode
}

func newRestoreMode(enabled bool) *atomic.Bool {
	mode := &atomic.Bool{}
	mode.Store(enabled)
	return mode
}

// restoreTerminalLocked restores the selected grid once per activation. Ordinary
// frames do not fight user resizing. renderMu serializes window requests with frames.
func (c *Client) restoreTerminalLocked(snap *protocolpb.Snapshot) {
	if c.isEmbedded() {
		return
	}
	if snap == nil {
		return
	}
	c.restoreState()
	size := terminal.WindowSize{Cols: int(snap.Cols), Rows: int(snap.Rows)}
	if c.isViewOnly() {
		c.restoreManual.Set(terminal.WindowSize{})
	} else {
		c.restoreManual.Observe(size)
	}
	geometryPending := c.restoreGeometryPending(size)
	if !c.restoreEnabled() || c.stdoutWriter() == io.Discard || geometryPending {
		return
	}
	cols, rows := size.Cols, size.Rows
	if cols < 1 || rows < 1 {
		return
	}
	localCols, localRows := c.terminalSize()
	if c.restoreInitialized {
		if c.restoreRequest.cols == localCols && c.restoreRequest.rows == localRows {
			c.restoreLocal = terminal.WindowSize{Cols: localCols, Rows: localRows}
			c.restoreRequest = restoreRequest{}
		}
		return
	}
	c.restoreInitialized = true
	c.restoreLocal = terminal.WindowSize{Cols: localCols, Rows: localRows}
	if manual := c.restoreManual.Pending(); manual.Cols > 0 && manual.Rows > 0 {
		cols, rows = manual.Cols, manual.Rows
	}
	pending := c.restoreWindow.latest()
	target := terminal.WindowSize{Cols: cols, Rows: rows}
	if cols == localCols && rows == localRows && (pending == (terminal.WindowSize{}) || pending == target) {
		c.restoreRequest = restoreRequest{}
		return
	}
	request := restoreRequest{cols, rows, localCols, localRows}
	c.restoreRequest = request
	if err := writeAll(c.clock(), c.stdoutWriter(), terminal.WindowResizeSequence(target)); err != nil {
		return
	}
	c.restoreWindow.requested(target)
	scheduler, ctx := c.effectRuntime()
	scheduler.Schedule(ctx, "restore-size", time.Second, func() {
		c.renderMu.Lock()
		if !c.restoreEnabled() || c.stdoutWriter() == io.Discard || c.restoreRequest != request {
			c.renderMu.Unlock()
			return
		}
		gotCols, gotRows := c.terminalSize()
		if gotCols == cols && gotRows == rows {
			c.renderMu.Unlock()
			return
		}
		c.ensureCompositor().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
			Kind: mvu.StatusError, Message: fmt.Sprintf("Restore: terminal is %dx%d; remote remains %dx%d", gotCols, gotRows, cols, rows), Duration: 5 * time.Second,
		}})
		c.renderMu.Unlock()
		c.scheduleRedrawEffect(mvu.EffectKeyStateExpiry, c.clock().Now(), 5*time.Second, false)
		c.requestRenderCurrent()
	})
}

func (c *Client) toggleRestore() bool {
	mode := c.restoreState()
	enabled := !mode.Load()
	mode.Store(enabled)
	if enabled && c.geometry != nil {
		c.geometry.request.Set(terminal.WindowSize{})
	}
	c.renderMu.Lock()
	c.restoreRequest = restoreRequest{}
	c.restoreInitialized = false
	c.restoreManual.Set(terminal.WindowSize{})
	cols, rows := c.terminalSize()
	c.restoreLocal = terminal.WindowSize{Cols: cols, Rows: rows}
	c.renderMu.Unlock()
	return enabled
}

// processTerminalResize distinguishes acknowledgement of our window request
// from a user resize. Only a user resize changes the selected remote session.
func (c *Client) processTerminalResize(ctx context.Context, allowNormal bool) error {
	c.restoreState()
	c.renderMu.Lock()
	cols, rows := c.terminalSize()
	actual := terminal.WindowSize{Cols: cols, Rows: rows}
	manual := false
	acknowledged := c.restoreWindow.acknowledge(actual)
	if acknowledged {
		c.restoreLocal = actual
		if c.restoreRequest.cols == cols && c.restoreRequest.rows == rows {
			c.restoreRequest = restoreRequest{}
		}
	} else if c.restoreEnabled() && cols > 0 && rows > 0 && c.stdoutWriter() != io.Discard {
		if c.restoreLocal != actual {
			c.cancelRestoreGeometry()
			c.restoreInitialized = true
			c.restoreRequest = restoreRequest{}
			c.restoreLocal = actual
			c.restoreManual.Set(actual)
			manual = true
		}
	}
	c.renderMu.Unlock()
	var err error
	if manual {
		err = c.applyRestoreManualResize(ctx)
	} else if !acknowledged && !c.restoreEnabled() && allowNormal {
		err = c.SendResize(ctx, cols, rows)
	}
	c.RenderCurrentFull()
	return err
}

func (c *Client) applyRestoreManualResize(ctx context.Context) error {
	if !c.restoreEnabled() {
		return nil
	}
	c.renderMu.Lock()
	if c.isViewOnly() {
		c.restoreManual.Set(terminal.WindowSize{})
		c.renderMu.Unlock()
		return nil
	}
	size := c.restoreManual.Pending()
	c.renderMu.Unlock()
	if size.Cols <= 0 || size.Rows <= 0 {
		return nil
	}
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return fmt.Errorf("client not connected")
	}
	frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(size.Cols), Rows: uint32(size.Rows), Explicit: true}}}
	return c.writeFrame(ctx, ws, frame)
}
