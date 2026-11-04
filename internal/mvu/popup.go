package mvu

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/mattn/go-runewidth"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/render"
)

// Popup is the immutable terminal layer beneath the usual UI overlays.
type Popup struct {
	SessionID string
	Name      string
	Snapshot  *protocolpb.Snapshot
}

// PopupRows returns terminal rows, leaving one header row inside the covered area.
// A viewport too small for a header and a terminal has no popup area.
func PopupRows(rows int) int { return max(0, min(24, rows/2)-1) }

type popupEntry struct {
	Popup
	hidden bool
}

// Popups holds UI-local parent associations. Sessions remain ordinary sessions.
// Snapshots are immutable copies so composition never acquires transport locks.
type Popups struct {
	mu      sync.RWMutex
	parents map[string]popupEntry
}

// Add associates a newly created session with its parent and shows it.
func (p *Popups) Add(parent string, popup Popup) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.parents == nil {
		p.parents = make(map[string]popupEntry)
	}
	p.parents[parent] = popupEntry{Popup: popup}
}

// Toggle changes visibility, returning false when the tab has no popup yet.
func (p *Popups) Toggle(parent string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.parents[parent]
	if ok {
		e.hidden = !e.hidden
		p.parents[parent] = e
	}
	return ok
}

// ID returns the associated session, including hidden popups.
func (p *Popups) ID(parent string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.parents[parent].SessionID
}

// Parent returns a popup's parent, or an empty string for normal sessions.
func (p *Popups) Parent(id string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for parent, e := range p.parents {
		if e.SessionID == id {
			return parent
		}
	}
	return ""
}

// Focus returns the shown popup, otherwise its parent.
func (p *Popups) Focus(parent string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if e, ok := p.parents[parent]; ok && !e.hidden {
		return e.SessionID
	}
	return parent
}

// Frame snapshots a visible popup for composition.
func (p *Popups) Frame(parent string) *Popup {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.parents[parent]
	if !ok || e.hidden {
		return nil
	}
	frame := e.Popup
	return &frame
}

// Update stores a detached snapshot and current display name.
func (p *Popups) Update(id, name string, snapshot *protocolpb.Snapshot) {
	if snapshot != nil {
		snapshot = proto.Clone(snapshot).(*protocolpb.Snapshot)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for parent, e := range p.parents {
		if e.SessionID == id {
			if name != "" {
				e.Name = name
			}
			if snapshot != nil {
				e.Snapshot = snapshot
			}
			p.parents[parent] = e
		}
	}
}

// Remove forgets a terminated session and promotes any surviving child to a tab.
func (p *Popups) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.parents, id)
	for parent, e := range p.parents {
		if e.SessionID == id {
			delete(p.parents, parent)
		}
	}
}

func popupLabel(name string) string {
	// Names can originate outside this UI; never interpret controls as chrome.
	name = strings.Map(func(r rune) rune {
		if r < ' ' || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, name)
	return name + "  Ctrl+L: j hide · J terminate · h help"
}

func composePopup(base *protocolpb.Snapshot, cols, rows int, state State) (*protocolpb.Snapshot, error) {
	height := PopupRows(rows)
	if height == 0 || cols <= 0 {
		return base, nil
	}
	normalize := func(snap *protocolpb.Snapshot, gridRows int) (*protocolpb.Snapshot, error) {
		if snap == nil {
			snap = BlankSnapshot(cols, gridRows)
		}
		var buf bytes.Buffer
		if err := render.SnapshotViewportNoClear(&buf, snap, cols, gridRows); err != nil {
			return nil, err
		}
		return composeLayersToSnapshot(cols, gridRows, buf.Bytes())
	}
	frame, err := normalize(base, rows)
	if err != nil {
		return nil, err
	}
	pane, err := normalize(state.Popup.Snapshot, height)
	if err != nil {
		return nil, err
	}
	label := runewidth.Truncate(popupLabel(state.Popup.Name), cols, "")
	header, err := composeLayersToSnapshot(cols, 1, []byte(fmt.Sprintf("\x1b[?7l\x1b[H%s%s%s%s%s", state.Theme.Reset, state.Theme.TabActiveBg, state.Theme.TabActiveFg, runewidth.FillRight(label, cols), state.Theme.Reset)))
	if err != nil {
		return nil, err
	}
	start := rows - height - 1
	copyPopupCells(frame, start, header)
	copyPopupCells(frame, start+1, pane)
	frame.Cursor = &protocolpb.Cursor{X: pane.GetCursor().GetX(), Y: uint32(start+1) + pane.GetCursor().GetY()}
	frame.CursorVisible = state.Popup.Snapshot != nil && pane.CursorVisible
	frame.Mode = pane.Mode
	return frame, nil
}

func copyPopupCells(dst *protocolpb.Snapshot, row int, src *protocolpb.Snapshot) {
	start := row * int(dst.Cols)
	copy(dst.Runes[start:], src.Runes)
	copy(dst.Modes[start:], src.Modes)
	copy(dst.Fg[start:], src.Fg)
	copy(dst.Bg[start:], src.Bg)
	if len(src.Graphemes) != 0 && len(dst.Graphemes) == 0 {
		dst.Graphemes = make([]string, len(dst.Runes))
	}
	if len(dst.Graphemes) > 0 {
		clear(dst.Graphemes[start : start+len(src.Runes)])
		copy(dst.Graphemes[start:], src.Graphemes)
	}
}
