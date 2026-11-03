package mvu

// ScrollbackPosition identifies history advancement independently of retained
// row count. A generation change denotes replacement rather than appended rows.
type ScrollbackPosition struct {
	Generation uint64
	End        int
}

// SyncHistory keeps the viewed rows in place as live output advances history.
// The storage owner then calls Normalize to clamp an evicted anchor to retained
// history. Inactive views continue to follow live output normally.
func (v *ScrollbackViewport) SyncHistory(position ScrollbackPosition) {
	if v == nil {
		return
	}
	if v.historyKnown && (position.Generation < v.history.Generation ||
		(position.Generation == v.history.Generation && position.End < v.history.End)) {
		return
	}
	if v.active && v.historyKnown && position.Generation == v.history.Generation && position.End >= v.history.End {
		v.offset += position.End - v.history.End
	}
	v.history, v.historyKnown = position, true
}

// Position reports cumulative appended rows, including rows already evicted.
func (b *ProtoScrollbackBuffer) Position() ScrollbackPosition {
	if b == nil {
		return ScrollbackPosition{}
	}
	return b.position
}
