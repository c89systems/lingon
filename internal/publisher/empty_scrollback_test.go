package publisher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relaywire"
	"pkt.systems/lingon/internal/terminal"
)

func TestEmptyScrollbackClearsPreviousTerminalBeforeSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
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
	var peer *websocket.Conn
	select {
	case peer = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p := New(Options{SessionID: "reused", Cols: 80, Rows: 24, Clock: clock.NewMock()})
	p.SetScrollbackSnapshot(func() []terminal.ScrollbackRow { return nil })
	p.Publish(nil, &protocolpb.Snapshot{Cols: 80, Rows: 24, Title: "replacement"})
	p.setConn(ws)
	done := make(chan error, 1)
	go func() { done <- p.readWS(ctx, ws) }()
	t.Cleanup(func() { cancel(); _ = ws.CloseNow(); _ = peer.CloseNow(); <-done })
	if err := relaywire.WriteFrame(ctx, peer, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{}}}); err != nil {
		t.Fatal(err)
	}
	first, err := relaywire.ReadFrame(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	if clear := first.GetScrollback(); clear == nil || !clear.Clear || len(clear.Rows) != 0 || clear.Cols != 80 {
		t.Fatalf("empty authoritative scrollback did not clear the previous terminal before snapshot: %v", first)
	}
	second, err := relaywire.ReadFrame(ctx, peer)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetSnapshot().GetTitle() != "replacement" {
		t.Fatalf("missing replacement snapshot: %v", second)
	}
}
