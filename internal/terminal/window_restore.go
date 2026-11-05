package terminal

import (
	"sync"
	"sync/atomic"
)

// WindowRestore coordinates restore mode and resize acknowledgements for a
// physical terminal shared by local and remote views. Requests outlive their
// selected tab and remain pending when its client is hidden. Observing a newer
// request also acknowledges older
// ones whose SIGWINCH events were coalesced by the terminal.
type WindowRestore struct {
	enabled atomic.Bool
	mu      sync.Mutex
	pending []WindowSize
}

// Requested records a programmatic window resize before its acknowledgement.
func (w *WindowRestore) Requested(size WindowSize) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Repeating the same target need not produce another SIGWINCH. Keep only
	// one consecutive request so it cannot later swallow a real user resize.
	if len(w.pending) > 0 && w.pending[len(w.pending)-1] == size {
		return
	}
	w.pending = append(w.pending, size)
}

// Acknowledge consumes a matching resize, including older coalesced requests.
func (w *WindowRestore) Acknowledge(size WindowSize) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, pending := range w.pending {
		if pending == size {
			w.pending = w.pending[i+1:]
			return true
		}
	}
	return false
}

// Latest returns the newest unacknowledged request.
func (w *WindowRestore) Latest() WindowSize {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return WindowSize{}
	}
	return w.pending[len(w.pending)-1]
}

// NewWindowRestore creates window-wide restore state, disabled by default.
func NewWindowRestore(enabled bool) *WindowRestore {
	w := &WindowRestore{}
	w.Store(enabled)
	return w
}

// Load reports whether session-size restoration is enabled.
func (w *WindowRestore) Load() bool { return w.enabled.Load() }

// Store enables or disables restoration without discarding pending acknowledgements.
func (w *WindowRestore) Store(enabled bool) { w.enabled.Store(enabled) }
