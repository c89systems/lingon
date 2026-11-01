package attach

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/terminal"
)

// All tabs share the initial window request, including its acknowledgement.
type clientGeometry struct {
	request   terminal.WindowSizeRequest
	mu        sync.Mutex
	requested bool
}

func newClientGeometry(cols, rows, localCols, localRows int, restore bool) (*clientGeometry, error) {
	if restore {
		return nil, nil
	}
	if cols < 0 || rows < 0 {
		return nil, fmt.Errorf("columns and rows must be zero or greater")
	}
	if cols == 0 && rows == 0 {
		return nil, nil
	}
	if cols == 0 {
		cols = localCols
	}
	if rows == 0 {
		rows = localRows
	}
	if cols <= 0 {
		cols = config.DefaultTerminalCols
	}
	if rows <= 0 {
		rows = config.DefaultTerminalRows
	}
	g := &clientGeometry{}
	g.request.Set(terminal.WindowSize{Cols: cols, Rows: rows})
	return g, nil
}

func (c *Client) geometrySize(cols, rows int) (int, int) {
	if c.geometry == nil {
		return cols, rows
	}
	size := c.geometry.request.Observe(terminal.WindowSize{Cols: cols, Rows: rows})
	return size.Cols, size.Rows
}

func (c *Client) applyGeometry(ctx context.Context) error {
	if c.geometry == nil || c.restoreEnabled() {
		return nil
	}
	c.renderMu.Lock()
	if c.stdoutWriter() == io.Discard {
		c.renderMu.Unlock()
		return nil
	}
	g := c.geometry
	g.mu.Lock()
	size := g.request.Pending()
	request := !g.requested && size.Cols > 0 && size.Rows > 0
	g.requested = true
	g.mu.Unlock()
	cols, rows := c.terminalSize()
	if size.Cols > 0 && size.Rows > 0 && (cols != size.Cols || rows != size.Rows) {
		if request {
			if err := writeAll(c.clock(), c.stdoutWriter(), terminal.WindowResizeSequence(size)); err != nil {
				c.renderMu.Unlock()
				return err
			}
		}
		scheduler, effectCtx := c.effectRuntime()
		scheduler.Schedule(effectCtx, "geometry-size", time.Second, func() {
			c.renderMu.Lock()
			if c.restoreEnabled() || c.stdoutWriter() == io.Discard || g.request.Pending() != size {
				c.renderMu.Unlock()
				return
			}
			actualCols, actualRows := c.terminalSize()
			if actualCols == size.Cols && actualRows == size.Rows {
				c.renderMu.Unlock()
				return
			}
			c.ensureCompositor().ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{Kind: mvu.StatusError, Message: fmt.Sprintf("Geometry: terminal is %dx%d; remote remains %dx%d", actualCols, actualRows, size.Cols, size.Rows), Duration: 5 * time.Second}})
			c.renderMu.Unlock()
			c.scheduleRedrawEffect(mvu.EffectKeyStateExpiry, c.clock().Now(), 5*time.Second, false)
			c.requestRenderCurrent()
		})
	}
	c.renderMu.Unlock()
	return c.SendResize(ctx, cols, rows)
}
