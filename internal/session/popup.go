package session

import (
	"os"
	"pkt.systems/lingon/internal/mvu"
)

func (r *Runner) focusedViewportSize(stdout, stdin *os.File) (int, int) {
	cols, rows := termSizeAny(stdout, stdin)
	if id, _ := r.focusedSession(); r.popups.Parent(id) != "" {
		rows = max(1, mvu.PopupRows(rows))
	}
	return cols, rows
}

// focusedSession separates input focus from the parent tab's identity.
func (r *Runner) focusedSession() (string, bool) {
	id, local := r.activeSession()
	if focused := r.popups.Focus(id); focused != id {
		return focused, r.isLocalSession(focused)
	}
	return id, local
}

func (r *Runner) resizePopup(cols, rows int) {
	parent, _ := r.activeSession()
	id := r.popups.ID(parent)
	if local := r.localSession(id); local != nil {
		height := mvu.PopupRows(rows)
		if height > 0 {
			currentCols, currentRows := local.Size()
			if currentCols != cols || currentRows != height {
				r.resizeLocalSession(local, cols, height)
			}
		}
	}
}
