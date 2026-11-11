package terminal

import "testing"

func TestWindowRestoreAcknowledgementsSurviveModeChanges(t *testing.T) {
	w := NewWindowRestore(false)
	if w.Load() || w.Latest() != (WindowSize{}) {
		t.Fatal("restore is not off and empty by default")
	}
	first, second := WindowSize{Cols: 100, Rows: 30}, WindowSize{Cols: 95, Rows: 28}
	w.Store(true)
	w.Requested(first)
	w.Requested(first) // Identical consecutive requests generate only one event.
	w.Requested(second)
	w.Store(false)
	if w.Load() || !w.Acknowledge(first) || w.Acknowledge(first) || w.Latest() != second {
		t.Fatal("mode change lost or duplicated a pending acknowledgement")
	}
	if !w.Acknowledge(second) || w.Latest() != (WindowSize{}) || w.Acknowledge(second) {
		t.Fatal("completed window request swallowed a later manual resize")
	}
	w.Requested(first)
	w.Requested(second)
	if !w.Acknowledge(second) || w.Acknowledge(first) {
		t.Fatal("coalesced acknowledgement retained an obsolete request")
	}
}
