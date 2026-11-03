package attach

import (
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestSeedFromRetainsOnlySameSessionPendingResize(t *testing.T) {
	for _, tc := range []struct {
		name, session string
		pending, want terminal.WindowSize
	}{
		{"same session", "alpha", terminal.WindowSize{Cols: 88, Rows: 26}, terminal.WindowSize{Cols: 88, Rows: 26}},
		{"different session", "beta", terminal.WindowSize{Cols: 88, Rows: 26}, terminal.WindowSize{Cols: 95, Rows: 28}},
		{"already acknowledged", "alpha", terminal.WindowSize{}, terminal.WindowSize{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// State transfer only: no terminal, renderer or process is started.
			previous := &Client{SessionID: "alpha", ClientID: "identity"}
			previous.restoreState()
			previous.restoreManual.Set(tc.pending)
			next := &Client{SessionID: tc.session}
			next.restoreState()
			next.restoreManual.Set(terminal.WindowSize{Cols: 95, Rows: 28})
			next.SeedFrom(previous)
			if next.restoreManual.Pending() != tc.want {
				t.Fatalf("pending resize %+v, want %+v", next.restoreManual.Pending(), tc.want)
			}
			if next.ClientID != "identity" {
				t.Fatal("client identity was lost during reconnect")
			}
		})
	}
}
