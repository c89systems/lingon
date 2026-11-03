package terminal

import "testing"

func TestWindowSizeRequestAcknowledgement(t *testing.T) {
	var request WindowSizeRequest
	actual := WindowSize{Cols: 80, Rows: 24}
	want := WindowSize{Cols: 100, Rows: 50}
	if got := request.Observe(actual); got != actual {
		t.Fatal(got)
	}
	request.Set(want)
	for _, size := range []WindowSize{actual, {Cols: 100, Rows: 24}, {Cols: 80, Rows: 50}} {
		if got := request.Observe(size); got != want {
			t.Fatalf("unacknowledged request lost: %v", got)
		}
	}
	if got := request.Observe(want); got != want || request.Pending() != (WindowSize{}) {
		t.Fatal("exact acknowledgement did not clear request")
	}
	if got := request.Observe(actual); got != actual {
		t.Fatalf("manual resize blocked after acknowledgement: %v", got)
	}
	request.Set(want)
	request.Set(WindowSize{})
	if got := request.Observe(actual); got != actual {
		t.Fatal("cancelled request retained")
	}
	if got := string(WindowResizeSequence(want)); got != "\x1b[8;50;100t" {
		t.Fatalf("resize sequence %q", got)
	}
}
