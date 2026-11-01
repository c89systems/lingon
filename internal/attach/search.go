package attach

import "pkt.systems/lingon/internal/mvu"

// renderSearch also handles snapshot-less connecting views. Preserve the
// displayed baseline once, so cancellation restores the last row even if no
// remote frame has arrived yet. Live snapshots always take precedence.
func (c *Client) renderSearch(state mvu.SearchState) {
	c.renderMu.Lock()
	defer c.renderMu.Unlock()
	if state.Visible && !c.ensureCompositor().Read().Search.Visible {
		c.searchBaseline = cloneSnapshot(c.renderCache.PrevSnapshot)
		if c.searchBaseline == nil {
			c.searchBaseline = c.snapshotOrBlank()
		}
	}
	c.ensureCompositor().ApplyAction(mvu.SearchAction{Search: state})
	snap := c.getSnapshot()
	if snap == nil {
		snap = c.searchBaseline
	}
	if snap == nil {
		snap = c.snapshotOrBlank()
	}
	c.renderSnapshotLocked(snap)
	if !state.Visible {
		c.searchBaseline = nil
	}
}
