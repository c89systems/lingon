package mvu

import (
	"fmt"

	"pkt.systems/version"
)

// HelpTitle returns the help modal title with the current Lingon version.
func HelpTitle() string {
	return fmt.Sprintf("lingon %s", version.Current())
}

// HelpCopyright is the copyright notice in terminal help.
const HelpCopyright = "Copyright (C) 2026 C89 Systems AB https://c89.systems"

// HelpLines returns the help modal content lines.
func HelpLines(state State) []string {
	return []string{
		HelpTitle(),
		HelpCopyright,
		"endpoint: " + state.Endpoint,
		"",
		"Ctrl+L h  help (also Ctrl+L Ctrl+H)",
		"Ctrl+L c  new session",
		"Ctrl+L j  show/hide popup terminal",
		"Ctrl+L J  terminate popup terminal",
		"Ctrl+L e  rename session",
		"Ctrl+L f  working directory (directories only; Tab/Shift+Tab cycle, Enter saves, Esc cancels)",
		"Ctrl+L /  find important session (all sessions if none marked; Tab/Shift+Tab cycle, Enter opens, Esc cancels)",
		"Ctrl+L ?  find session (always all tabs)",
		"Ctrl+L i  important tabs (also Ctrl+L Ctrl+I; Tab cycles, Space adds, Ctrl+W deletes name, Enter toggles, Esc cancels)",
		"Ctrl+L I  reset important tabs",
		"Ctrl+L Q  close session",
		"Ctrl+L q  quit attach (sessions keep running)",
		"Ctrl+L C  send Enter ~. (disconnect SSH)",
		"Ctrl+L d  send Ctrl+D",
		"Ctrl+L ]  paste copied text into focused terminal",
		"Ctrl+L [  scrollback (v visual lines; Space select; Space/Enter copy; PgUp/PgDn/w/s/u/d half page; Up/Down or j/k line; J/K five lines; h/l one col outside visual; H/L five cols; Home/End/g/G top/bottom; wheel scrolls; q leaves)",
		"Ctrl+L r  toggle respawn",
		"Ctrl+L 0  resize active headless to viewport",
		"Ctrl+L !  toggle remote-size restore (attach only)",
		"Ctrl+L o  toggle offline (host local-only)",
		"Ctrl+L w  cycle inactivity wall",
		"Ctrl+L n / p  next/prev tab (important tabs only in important mode)",
		"Ctrl+L b  toggle tab bar",
		"Ctrl+L t  next theme",
		"Ctrl+L Ctrl+L  send literal Ctrl+L",
		"",
		"press q or Q to close help",
	}
}
