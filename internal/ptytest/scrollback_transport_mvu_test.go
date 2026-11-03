package ptytest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

// The controlled peer deliberately separates history from live snapshots, and
// sends complete replacement snapshots with unchanged cells. The viewer owns
// its terminal; neither the HTTP server nor this test uses inherited stdio.
func TestScrollbackTransportFramesAreReducedBeforePrintingPTY(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	frames := make(chan *protocolpb.Frame)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer close(done)
		defer ws.CloseNow()
		if _, _, err := ws.Read(ctx); err != nil {
			return
		}
		seq := uint64(0)
		for {
			select {
			case frame := <-frames:
				seq++
				frame.Seq = seq
				frame.SessionId = "transport"
				data, err := proto.Marshal(frame)
				if err != nil {
					return
				}
				if err := ws.Write(ctx, websocket.MessageBinary, data); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}))
	t.Cleanup(func() {
		cancel()
		server.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("controlled peer did not stop")
		}
	})
	send := func(f *protocolpb.Frame) {
		t.Helper()
		select {
		case frames <- f:
		case <-time.After(8 * time.Second):
			t.Fatal("controlled peer blocked")
		}
	}
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	terminal := h.StartAttach(ptytest.AttachOptions{Endpoint: server.URL, SessionID: "transport", Cols: 60, Rows: 12})
	t.Cleanup(terminal.Cancel)
	history := make([]*protocolpb.ScrollbackRow, 80)
	for y := range history {
		for _, r := range fmt.Sprintf("HISTORY-%02d", y) {
			history[y] = appendScrollRune(history[y], r)
		}
	}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: history, Clear: true}}})
	live := mvu.BlankSnapshot(60, 12)
	live.Cursor = &protocolpb.Cursor{Y: 11}
	live.Title = "FRAME-1"
	for y := range 12 {
		setTransportRow(live, y, fmt.Sprintf("ORIGINAL-%02d", y))
	}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: live}})
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("ORIGINAL-11") })
	terminal.Send("\x0c[")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[100%]") })
	before := terminal.Screen()
	terminal.DrainRaw()
	displaced := &protocolpb.ScrollbackRow{Runes: append([]uint32(nil), live.Runes[:60]...), Fg: append([]uint32(nil), live.Fg[:60]...), Bg: append([]uint32(nil), live.Bg[:60]...)}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: []*protocolpb.ScrollbackRow{displaced}}}})
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[99%]") })
	after := terminal.Screen()
	for row := 1; row < 12; row++ {
		if before.Row(row) != after.Row(row) {
			t.Fatalf("history-only update scrolled live row %d: %q -> %q", row, before.Row(row), after.Row(row))
		}
	}
	if raw := terminal.DrainRaw(); strings.Contains(raw, "ORIGINAL-") {
		t.Fatalf("history-only update repainted stable live cells: %q", raw)
	}
	next := mvu.BlankSnapshot(60, 12)
	next.Cursor = &protocolpb.Cursor{Y: 11}
	next.Title = "FRAME-2"
	for y := range 12 {
		setTransportRow(next, y, fmt.Sprintf("ORIGINAL-%02d", y+1))
	}
	setTransportRow(next, 10, "EDITED-IN-PLACE")
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(11), "EDITED-IN-PLACE") })
	if row := terminal.Screen().Row(1); row != before.Row(1) {
		t.Fatalf("live edit scrolled the viewport: %q", row)
	}
	if raw := terminal.DrainRaw(); strings.Contains(raw, "ORIGINAL-") || strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("live edit repainted unchanged content: %q", raw)
	}
	terminal.Send("g")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(1), "HISTORY-01") && strings.Contains(s.Row(0), "[0%]")
	})
	terminal.DrainRaw()
	// A title change fences printing even though every visible cell is unchanged.
	next = proto.Clone(next).(*protocolpb.Snapshot)
	next.Title = "FRAME-3"
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	waitTransportTitle(t, terminal, "FRAME-3")
	if raw := terminal.DrainRaw(); strings.Contains(raw, "HISTORY-") || strings.Contains(raw, "[0%]") {
		t.Fatalf("unchanged historical View emitted text: %q", raw)
	}
	terminal.Send("q")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return !strings.Contains(s.Row(0), "%]") && s.Contains("EDITED-IN-PLACE") })
	terminal.DrainRaw()
	next = proto.Clone(next).(*protocolpb.Snapshot)
	next.Title = "FRAME-4"
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	waitTransportTitle(t, terminal, "FRAME-4")
	if raw := terminal.DrainRaw(); strings.Contains(raw, "ORIGINAL-") || strings.Contains(raw, "EDITED-IN-PLACE") || strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("identical replacement snapshot repainted the normal screen: %q", raw)
	}
}

func appendScrollRune(row *protocolpb.ScrollbackRow, r rune) *protocolpb.ScrollbackRow {
	if row == nil {
		row = &protocolpb.ScrollbackRow{}
	}
	row.Runes = append(row.Runes, uint32(r))
	return row
}
func setTransportRow(s *protocolpb.Snapshot, y int, text string) {
	for x, r := range text {
		s.Runes[y*int(s.Cols)+x] = uint32(r)
	}
}
func waitTransportTitle(t *testing.T, terminal *ptytest.PTYSession, title string) {
	t.Helper()
	waitSearchScreen(t, terminal, func(_ ptytest.Screen) bool { return terminal.Snapshot().Title == title })
}
