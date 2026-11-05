package mvu

import (
	"maps"
	"time"
)

// ImportantTabs is an immutable, UI-local set of marked session identities.
// Updates copy the set so existing frame/read snapshots remain unchanged.
type ImportantTabs struct{ ids map[string]bool }

// Active reports whether important-tab mode has been entered.
func (tabs ImportantTabs) Active() bool { return len(tabs.ids) > 0 }

// Contains reports whether the session has been marked in this UI process.
func (tabs ImportantTabs) Contains(id string) bool { return tabs.ids[id] }

// ImportantTabAction marks a parent tab, or resets all marks when Reset is true.
// Marking an already important tab is idempotent. Session-list updates do not
// reset the mode; only the explicit reset command does.
type ImportantTabAction struct {
	SessionID string
	Reset     bool
}

func (a ImportantTabAction) run(r *Runtime, _ time.Time) ActionResult {
	changed := false
	r.mutateState(func(state *State, _ time.Time) {
		if a.Reset {
			changed = state.ImportantTabs.Active()
			state.ImportantTabs = ImportantTabs{}
		} else if a.SessionID != "" && !state.ImportantTabs.Contains(a.SessionID) {
			ids := maps.Clone(state.ImportantTabs.ids)
			if ids == nil {
				ids = make(map[string]bool)
			}
			ids[a.SessionID] = true
			state.ImportantTabs = ImportantTabs{ids: ids}
			changed = true
		}
	})
	return ActionResult{Changed: changed}
}
