package mvu

// ScrollbackPosition identifies history advancement independently of retained
// row count. A generation change denotes replacement rather than appended rows.
type ScrollbackPosition struct {
	Generation uint64
	End        int
}

// OffsetFor projects the stored anchor onto a history snapshot without mutating
// the viewport. Output updates rebase the anchor; concurrent redraws may observe
// committed history before its notification has run.
func (v *ScrollbackViewport) OffsetFor(position ScrollbackPosition) int {
	if v == nil {
		return 0
	}
	offset := v.offset
	if v.active && v.historyKnown && position.Generation == v.history.Generation {
		offset += max(0, position.End-v.history.End)
	}
	return offset
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
