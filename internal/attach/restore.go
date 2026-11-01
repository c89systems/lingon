package attach

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
)

type restoreRequest struct{ cols, rows, localCols, localRows int }

func (c *Client) restoreEnabled() bool {
	return c.restoreMode != nil && c.restoreMode.Load()
}

func newRestoreMode(enabled bool) *atomic.Bool {
	mode := &atomic.Bool{}
	mode.Store(enabled)
	return mode
}

// restoreTerminalLocked requests the remote grid in character cells. Only the
// visible client may issue window operations; renderMu serializes them with frames.
func (c *Client) restoreTerminalLocked(snap *protocolpb.Snapshot) {
	if !c.restoreEnabled() || c.stdoutWriter() == io.Discard || snap == nil {
		return
	}
	cols, rows := int(snap.Cols), int(snap.Rows)
	if cols < 1 || rows < 1 {
		return
	}
	localCols, localRows := c.terminalSize()
	if cols == localCols && rows == localRows {
		c.restoreRequest = restoreRequest{}
		return
	}
	request := restoreRequest{cols, rows, localCols, localRows}
	if c.restoreRequest == request {
		return
	}
	c.restoreRequest = request
	sequence := fmt.Sprintf("\x1b[8;%d;%dt", rows, cols)
	if err := writeAll(c.clock(), c.stdoutWriter(), []byte(sequence)); err != nil {
		return
	}
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
	enabled := !c.restoreMode.Load()
	c.restoreMode.Store(enabled)
	c.renderMu.Lock()
	c.restoreRequest = restoreRequest{}
	c.renderMu.Unlock()
	return enabled
}
