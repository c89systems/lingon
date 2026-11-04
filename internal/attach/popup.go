package attach

import "pkt.systems/lingon/internal/mvu"

func (c *Client) popupFrame() *mvu.Popup {
	if c.Popup != nil {
		return c.Popup()
	}
	return nil
}

func (c *Client) isEmbedded() bool { return c.embedded != nil && c.embedded() }

func popupTabs(sessions []SessionInfo, popups *mvu.Popups) []SessionInfo {
	tabs := make([]SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if popups.Parent(session.ID) == "" {
			tabs = append(tabs, session)
		}
	}
	return tabs
}
