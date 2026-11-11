package attach

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

// All tabs share the initial window request, including its acknowledgement.
type clientGeometry struct {
	request     terminal.WindowSizeRequest
	mu          sync.Mutex
	requested   bool
	restoreSeed *geometrySeed
}

// geometrySeed belongs only to the initially selected session. All mutable
// fields are protected by clientGeometry.mu; the pointer and size are immutable.
type geometrySeed struct {
	size              terminal.WindowSize
	sessionID         string
	assigned, applied bool
	sentTo            *websocket.Conn
}

func newClientGeometry(cols, rows, localCols, localRows int, restore bool) (*clientGeometry, error) {
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
	size := terminal.WindowSize{Cols: cols, Rows: rows}
	if restore {
		g.restoreSeed = &geometrySeed{size: size}
	} else {
		g.request.Set(size)
	}
	return g, nil
}

func (c *Client) geometrySize(cols, rows int) (int, int) {
	if c.isEmbedded() {
		return cols, rows
	}
	if c.geometry == nil || c.geometry.restoreSeed != nil {
		return cols, rows
	}
	size := c.geometry.request.Observe(terminal.WindowSize{Cols: cols, Rows: rows})
	return size.Cols, size.Rows
}

func (c *Client) applyGeometry(ctx context.Context) error {
	if c.isEmbedded() {
		return nil
	}
	if c.geometry == nil {
		return nil
	}
	if c.geometry.restoreSeed != nil {
		return c.applyRestoreGeometry(ctx)
	}
	if c.restoreEnabled() {
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

func (g *clientGeometry) bindInitialSession(id string) {
	if g == nil || g.restoreSeed == nil {
		return
	}
	g.mu.Lock()
	if !g.restoreSeed.assigned {
		g.restoreSeed.sessionID = id
		g.restoreSeed.assigned = true
	}
	g.mu.Unlock()
}

func (c *Client) applyRestoreGeometry(ctx context.Context) error {
	g := c.geometry
	g.bindInitialSession(c.SessionID)
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	// Recheck at delivery: a manual resize can supersede a queued startup size.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	g.mu.Lock()
	seed := g.restoreSeed
	if seed.sessionID != c.SessionID || seed.applied || c.isViewOnly() {
		g.mu.Unlock()
		return nil
	}
	if ws == nil {
		g.mu.Unlock()
		return fmt.Errorf("client not connected")
	}
	if seed.sentTo == ws {
		g.mu.Unlock()
		return nil
	}
	seed.sentTo = ws
	size := seed.size
	g.mu.Unlock()
	frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(size.Cols), Rows: uint32(size.Rows), Explicit: true}}}
	if err := writeFrame(ctx, ws, frame); err != nil {
		g.mu.Lock()
		if seed.sentTo == ws {
			seed.sentTo = nil
		}
		g.mu.Unlock()
		return err
	}
	c.touchActivity()
	return nil
}

func (c *Client) restoreGeometryPending(size terminal.WindowSize) bool {
	g := c.geometry
	if g == nil || g.restoreSeed == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	seed := g.restoreSeed
	if !seed.assigned || seed.sessionID != c.SessionID || seed.applied {
		return false
	}
	if seed.size == size || c.isViewOnly() {
		seed.applied = true
		return false
	}
	return true
}

func (c *Client) cancelRestoreGeometry() {
	g := c.geometry
	if g == nil || g.restoreSeed == nil {
		return
	}
	g.mu.Lock()
	if g.restoreSeed.assigned && g.restoreSeed.sessionID == c.SessionID {
		g.restoreSeed.applied = true
	}
	g.mu.Unlock()
}
