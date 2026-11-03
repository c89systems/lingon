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
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

// The controlled peer deliberately separates history from live snapshots, and
// sends complete replacement snapshots with unchanged cells. The viewer owns
// its terminal; neither the HTTP server nor this test uses inherited stdio.
func TestScrollbackHistoryAndLiveFramesStayAlignedPTY(t *testing.T) {
	for _, mode := range []string{"attach", "multi"} {
		for _, delivery := range []string{"snapshot", "diff", "enter-pending"} {
			t.Run(mode+"/"+delivery, func(t *testing.T) { testScrollbackFrameOrder(t, mode, delivery) })
		}
	}
}

func testScrollbackFrameOrder(t *testing.T, mode, delivery string) {
	ctx, cancel := context.WithCancel(t.Context())
	frames := make(chan *protocolpb.Frame)
	delivered := make(chan struct{})
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
		// A pong proves the application read loop finished the preceding frame.
		reading := make(chan struct{})
		go func() {
			defer close(reading)
			for {
				if _, _, err := ws.Read(ctx); err != nil {
					return
				}
			}
		}()
		defer func() { _ = ws.CloseNow(); <-reading }()
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
				pingCtx, stopPing := context.WithTimeout(ctx, 8*time.Second)
				err = ws.Ping(pingCtx)
				stopPing()
				if err != nil {
					return
				}
				select {
				case delivered <- struct{}{}:
				case <-ctx.Done():
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
		select {
		case <-delivered:
		case <-time.After(8 * time.Second):
			t.Fatal("frame was not processed")
		}
	}
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()))
	var terminal *ptytest.PTYSession
	if mode == "multi" {
		terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{Endpoint: server.URL, SessionID: "transport", Cols: 60, Rows: 12,
			SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
				return []attach.SessionInfo{{ID: "transport", Name: "transport", Status: "active"}}, nil
			},
		})
	} else {
		terminal = h.StartAttach(ptytest.AttachOptions{Endpoint: server.URL, SessionID: "transport", Cols: 60, Rows: 12})
	}
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
	if delivery == "enter-pending" {
		send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: []*protocolpb.ScrollbackRow{{Runes: append([]uint32(nil), live.Runes[:60]...), Fg: append([]uint32(nil), live.Fg[:60]...), Bg: append([]uint32(nil), live.Bg[:60]...)}}}}})
	}
	terminal.Send("\x0c[")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
		if delivery == "enter-pending" {
			return strings.Contains(s.Row(0), "[99%]")
		}
		return strings.Contains(s.Row(0), "[100%]")
	})
	before := terminal.Screen()
	terminal.DrainRaw()
	// Deliberately deliver multiple history-only messages before any new live
	// frame. Each message must preserve every visible row and avoid repainting it.
	const advanced = 3
	first := 0
	if delivery == "enter-pending" {
		first = 1
	}
	for n := first; n < advanced; n++ {
		displaced := &protocolpb.ScrollbackRow{Runes: append([]uint32(nil), live.Runes[n*60:(n+1)*60]...), Fg: append([]uint32(nil), live.Fg[n*60:(n+1)*60]...), Bg: append([]uint32(nil), live.Bg[n*60:(n+1)*60]...)}
		send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: []*protocolpb.ScrollbackRow{displaced}}}})
		percent := mvu.ScrollbackPercent(80+n+1+12, 12, n+1)
		waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), fmt.Sprintf("[%d%%]", percent)) })
		after := terminal.Screen()
		for row := 1; row < 12; row++ {
			if before.Row(row) != after.Row(row) {
				t.Fatalf("history-only update %d scrolled live row %d: %q -> %q", n, row, before.Row(row), after.Row(row))
			}
		}
		if raw := terminal.DrainRaw(); strings.Contains(raw, "ORIGINAL-") {
			t.Fatalf("history-only update repainted stable live cells: %q", raw)
		}
	}
	next := mvu.BlankSnapshot(60, 12)
	next.Cursor = &protocolpb.Cursor{Y: 11}
	next.Title = "FRAME-2"
	for y := range 12 {
		setTransportRow(next, y, fmt.Sprintf("ORIGINAL-%02d", y+advanced))
	}
	setTransportRow(next, 11-advanced, "EDITED-IN-PLACE")
	if delivery == "diff" {
		// Row three is unchanged relative to the canonical live grid, so it is
		// deliberately omitted. Shifting canonical input state would corrupt it.
		setTransportRow(next, 3, "ORIGINAL-03")
		diff := &protocolpb.Diff{Cols: 60, Rows: 12, Cursor: next.Cursor, CursorVisible: next.CursorVisible, Mode: next.Mode, Title: next.Title}
		for y := 0; y < 12; y++ {
			if y == 3 {
				continue
			}
			start, end := y*60, (y+1)*60
			diff.DiffRows = append(diff.DiffRows, &protocolpb.DiffRow{Row: uint32(y), Runes: next.Runes[start:end], Fg: next.Fg[start:end], Bg: next.Bg[start:end], Modes: next.Modes[start:end]})
		}
		send(&protocolpb.Frame{Payload: &protocolpb.Frame_Diff{Diff: diff}})
	} else {
		send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	}
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(11), "EDITED-IN-PLACE") })
	if row := terminal.Screen().Row(1); row != before.Row(1) {
		t.Fatalf("live edit scrolled the viewport: %q", row)
	}
	if raw := terminal.DrainRaw(); strings.Contains(raw, "ORIGINAL-01") || strings.Contains(raw, "ORIGINAL-02") || strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("live edit repainted unchanged content: %q", raw)
	}
	if delivery == "diff" && !strings.Contains(terminal.Screen().Row(3+advanced), "ORIGINAL-03") {
		t.Fatal("sparse diff used the shifted scrollback grid as its canonical baseline")
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
	// A history reset changes the coordinate generation. Pending rows from the
	// preceding generation must not affect the replacement or the canonical grid.
	replacement := make([]*protocolpb.ScrollbackRow, 4)
	for y := range replacement {
		for _, r := range fmt.Sprintf("REPLACEMENT-%02d", y) {
			replacement[y] = appendScrollRune(replacement[y], r)
		}
	}
	terminal.DrainRaw()
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: replacement, Clear: true}}})
	// Replacement history becomes visible with its terminating live snapshot.
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
		return strings.Contains(s.Row(1), "REPLACEMENT-01") && strings.Contains(s.Row(0), "[0%]")
	})
	if raw := terminal.DrainRaw(); strings.Contains(raw, "\x1b[2J") {
		t.Fatal("history reset cleared the physical screen")
	}
	// More pending rows than the complete live grid must still preserve a view
	// containing both historical and formerly live cells.
	batch := make([]*protocolpb.ScrollbackRow, 20)
	for y := range batch {
		if y < 12 {
			batch[y] = &protocolpb.ScrollbackRow{Runes: append([]uint32(nil), next.Runes[y*60:(y+1)*60]...), Fg: append([]uint32(nil), next.Fg[y*60:(y+1)*60]...), Bg: append([]uint32(nil), next.Bg[y*60:(y+1)*60]...)}
		} else {
			for _, r := range "BURST-NEW" {
				batch[y] = appendScrollRune(batch[y], r)
			}
		}
	}
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Cols: 60, Rows: batch}}})
	next = proto.Clone(next).(*protocolpb.Snapshot)
	next.Title = "FRAME-LARGE-BATCH"
	send(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: next}})
	waitTransportTitle(t, terminal, "FRAME-LARGE-BATCH")
	if raw := terminal.DrainRaw(); strings.Contains(raw, "REPLACEMENT-") || strings.Contains(raw, "ORIGINAL-") || strings.Contains(raw, "\x1b[2J") {
		t.Fatalf("batched history repainted pinned rows: %q", raw)
	}
	terminal.Send("q")
	waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return !strings.Contains(s.Row(0), "%]") && s.Contains("EDITED-IN-PLACE") })
}

func appendScrollRune(row *protocolpb.ScrollbackRow, r rune) *protocolpb.ScrollbackRow {
	if row == nil {
		row = &protocolpb.ScrollbackRow{}
	}
	row.Runes = append(row.Runes, uint32(r))
	return row
}
func setTransportRow(s *protocolpb.Snapshot, y int, text string) {
	clear(s.Runes[y*int(s.Cols) : (y+1)*int(s.Cols)])
	for x, r := range text {
		s.Runes[y*int(s.Cols)+x] = uint32(r)
	}
}
func waitTransportTitle(t *testing.T, terminal *ptytest.PTYSession, title string) {
	t.Helper()
	waitSearchScreen(t, terminal, func(_ ptytest.Screen) bool { return terminal.Snapshot().Title == title })
}
