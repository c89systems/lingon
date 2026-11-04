package attach

import (
	"context"

	"pkt.systems/lingon/internal/control"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/terminal"
)

func (c *Client) textClipboard() *control.TextClipboard {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.TextClipboard == nil {
		c.TextClipboard = &control.TextClipboard{}
	}
	return c.TextClipboard
}

func (c *Client) syncCopySourceLocked() {
	snap := c.scrollbackLive
	if snap == nil {
		snap = c.snapshotOrBlank()
	}
	c.scrollbackView.SyncSource(mvu.ScrollbackSource{Position: c.scrollbackBuffer.Position(), HistoryRows: c.scrollbackBuffer.Len(), LiveRows: int(snap.GetRows()), ViewRows: c.scrollbackViewRows()})
}

// SendTextPaste sends literal clipboard text using the current application's
// paste mode. It bypasses image-clipboard detection and prefix parsing.
func (c *Client) SendTextPaste(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	snap := c.Snapshot()
	return c.SendInput(ctx, terminal.TextPaste(text, snap.GetMode()))
}

// ScrollbackCopy toggles the visual cursor or advances the Space selection.
func (c *Client) ScrollbackCopy(selectLines bool) {
	clipboard := c.textClipboard()
	c.scrollbackMu.Lock()
	c.syncScrollbackLocked()
	if !c.scrollbackView.Active() {
		c.scrollbackMu.Unlock()
		return
	}
	snap := c.scrollbackLive
	if snap == nil {
		snap = c.snapshotOrBlank()
	}
	c.scrollbackView.SyncSource(mvu.ScrollbackSource{Position: c.scrollbackBuffer.Position(), HistoryRows: c.scrollbackBuffer.Len(), LiveRows: int(snap.GetRows()), ViewRows: c.scrollbackViewRows()})
	if !selectLines {
		c.scrollbackView.ToggleVisual()
	} else if rows, copy := c.scrollbackView.SelectLines(); copy {
		clipboard.Store(mvu.CopyScrollbackLines(mvu.NewProtoTextSource(c.scrollbackBuffer.Rows(), snap), rows))
	}
	c.scrollbackMu.Unlock()
}
