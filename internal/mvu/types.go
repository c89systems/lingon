package mvu

import (
	"time"

	"pkt.systems/lingon/internal/theme"
)

// Cursor describes a cursor position using 1-based screen coordinates.
type Cursor struct {
	Row     int
	Col     int
	Visible bool
}

// State captures UI state used by MVU layers.
type State struct {
	SessionID string
	Endpoint  string
	Theme     theme.TUITheme

	HelpVisible bool
	Search      SearchState

	ConnectionMessage   string
	ConnectionStyle     BannerStyle
	ConnectionShownAt   time.Time
	ConnectionExpiresAt time.Time

	LoadingMessage string

	DisconnectTitle    string
	DisconnectDetail   string
	DisconnectVisible  bool
	DisconnectBoxWidth int

	WallTitle     string
	WallMessage   string
	WallVisible   bool
	WallExpiresAt time.Time

	ScrollbackMessage string

	Tabs            []Tab
	ActiveTab       int
	TabBarVisible   bool
	TabBarShownAt   time.Time
	TabBarExpiresAt time.Time
}

// Tab describes a session tab.
type Tab struct {
	ID    string
	Index int
	Title string
	// Highlighted marks a search candidate independently of the active session.
	Highlighted bool
	// Disabled indicates the tab is paused/disconnected locally.
	Disabled bool
	// Muted indicates the tab should render with reduced emphasis.
	Muted bool
}
