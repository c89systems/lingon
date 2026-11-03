package terminal

import (
	"fmt"
	"sync"
)

// WindowResizeSequence requests a character-cell window size from an emulator.
// The emulator must update its PTY size to acknowledge the request.
func WindowResizeSequence(size WindowSize) []byte {
	return []byte(fmt.Sprintf("\x1b[8;%d;%dt", size.Rows, size.Cols))
}

// WindowSizeRequest keeps a requested viewport authoritative until the emulator
// acknowledges it. After acknowledgement, ordinary user-driven resizing resumes.
type WindowSizeRequest struct {
	mu   sync.Mutex
	size WindowSize
}

// Set records the requested dimensions. A zero size cancels a pending request.
func (r *WindowSizeRequest) Set(size WindowSize) {
	r.mu.Lock()
	r.size = size
	r.mu.Unlock()
}

// Pending returns the unacknowledged dimensions, or zero when none remain.
func (r *WindowSizeRequest) Pending() WindowSize {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

// Observe resolves a physical viewport into the effective terminal dimensions.
func (r *WindowSizeRequest) Observe(actual WindowSize) WindowSize {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size.Cols > 0 && r.size.Rows > 0 {
		requested := r.size
		if actual == requested {
			r.size = WindowSize{}
		}
		return requested
	}
	return actual
}
