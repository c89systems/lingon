package relayhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
)

func TestScrollbackReplacementPrecedesQueuedOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		accepted <- ws
		<-ctx.Done()
	}))
	defer server.Close()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	peer := <-accepted
	controls := make(chan *protocolpb.Frame, 2)
	controls <- &protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Clear: true}}}
	queue := relaywire.NewFrameQueue(0)
	queue.Enqueue(&protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: &protocolpb.Snapshot{Title: "ordinary-output"}}}, nil)
	done := make(chan error, 1)
	go func() { done <- (&Host{}).writeFrames(ctx, ws, controls, queue) }()
	t.Cleanup(func() { cancel(); _ = ws.CloseNow(); <-done })
	first, err := relaywire.ReadFrame(ctx, peer)
	if err != nil || !first.GetScrollback().GetClear() {
		t.Fatalf("replacement start: %v, %v", first, err)
	}
	// Release continuations only after the first chunk is on the wire. Ordinary
	// output is already queued, so a writer that mixes the queues fails reliably.
	controls <- &protocolpb.Frame{Payload: &protocolpb.Frame_Scrollback{Scrollback: &protocolpb.Scrollback{Rows: []*protocolpb.ScrollbackRow{{Runes: []uint32{'R'}}}}}}
	controls <- &protocolpb.Frame{Payload: &protocolpb.Frame_Snapshot{Snapshot: &protocolpb.Snapshot{Title: "replacement"}}}
	second, err := relaywire.ReadFrame(ctx, peer)
	if err != nil || second.GetScrollback() == nil {
		t.Fatalf("replacement interrupted by %v, %v", second, err)
	}
	for _, title := range []string{"replacement", "ordinary-output"} {
		frame, err := relaywire.ReadFrame(ctx, peer)
		if err != nil || frame.GetSnapshot().GetTitle() != title {
			t.Fatalf("want %s, got %v, %v", title, frame, err)
		}
	}
}
