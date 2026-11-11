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

// ImportantTabsAction toggles each parent identity once, atomically, or clears all
// marks when Reset is true.
// Removing the last mark leaves important mode. Session-list updates do not
// change the marks.
type ImportantTabsAction struct {
	SessionIDs []string
	Reset      bool
}

func (a ImportantTabsAction) run(r *Runtime, _ time.Time) ActionResult {
	changed := false
	r.mutateState(func(state *State, _ time.Time) {
		if a.Reset {
			changed = state.ImportantTabs.Active()
			state.ImportantTabs = ImportantTabs{}
		} else if len(a.SessionIDs) > 0 {
			ids := maps.Clone(state.ImportantTabs.ids)
			if ids == nil {
				ids = make(map[string]bool)
			}
			seen := make(map[string]bool)
			for _, id := range a.SessionIDs {
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				if ids[id] {
					delete(ids, id)
				} else {
					ids[id] = true
				}
				changed = true
			}
			state.ImportantTabs = ImportantTabs{ids: ids}
		}
	})
	return ActionResult{Changed: changed}
}

// NextSessionID selects the next/previous marked tab in the full tab order.
// Outside important mode it cycles all tabs. No remaining eligible tab keeps the
// current session selected, including while previously marked tabs are absent.
func (tabs ImportantTabs) NextSessionID(sessions []SessionTabSource, active string, dir int) string {
	if !tabs.Active() {
		return NextSessionID(sessions, active, dir)
	}
	return nextSessionID(sessions, active, dir, tabs.Contains)
}
