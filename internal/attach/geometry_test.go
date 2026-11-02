package attach

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func TestGeometryResolutionWithRestore(t *testing.T) {
	for _, tc := range []struct {
		cols, rows int
		restore    bool
		want       terminal.WindowSize
		invalid    bool
	}{
		{100, 50, false, terminal.WindowSize{Cols: 100, Rows: 50}, false},
		{100, 0, false, terminal.WindowSize{Cols: 100, Rows: 24}, false},
		{0, 50, false, terminal.WindowSize{Cols: 80, Rows: 50}, false},
		{0, 0, false, terminal.WindowSize{}, false},
		{100, 50, true, terminal.WindowSize{Cols: 100, Rows: 50}, false},
		{100, 0, true, terminal.WindowSize{Cols: 100, Rows: 24}, false},
		{0, 50, true, terminal.WindowSize{Cols: 80, Rows: 50}, false},
		{-1, -1, true, terminal.WindowSize{}, true},
		{-1, 50, false, terminal.WindowSize{}, true},
	} {
		g, err := newClientGeometry(tc.cols, tc.rows, 80, 24, tc.restore)
		if (err != nil) != tc.invalid {
			t.Fatalf("%+v: %v", tc, err)
		}
		var got terminal.WindowSize
		if g != nil {
			if g.restoreSeed != nil {
				got = g.restoreSeed.size
			} else {
				got = g.request.Pending()
			}
		}
		if got != tc.want {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func TestGeometryWarningAndAcknowledgement(t *testing.T) {
	for _, ack := range []bool{false, true} {
		clk := clock.NewMock()
		var output bytes.Buffer
		cols, rows := 80, 24
		g, err := newClientGeometry(100, 50, cols, rows, false)
		if err != nil {
			t.Fatal(err)
		}
		c := &Client{Clock: clk, Stdout: &output, geometry: g, restoreMode: newRestoreMode(false), TermSize: func() (int, int) { return cols, rows }}
		stop := c.startEffects(t.Context())
		c.ensureCompositor()
		// No socket is required to verify local output and the injected-clock warning.
		if err := c.applyGeometry(context.Background()); err == nil {
			t.Fatal("expected disconnected remote")
		}
		if got := output.String(); got != "\x1b[8;50;100t" {
			t.Fatalf("request %q", got)
		}
		_ = c.applyGeometry(context.Background())
		if got := output.String(); got != "\x1b[8;50;100t" {
			t.Fatalf("duplicate request %q", got)
		}
		if ack {
			cols, rows = 100, 50
			c.geometrySize(cols, rows)
		}
		clk.Add(999 * time.Millisecond)
		if msg := c.compositor.Read().ConnectionMessage; msg != "" {
			t.Fatalf("early warning %q", msg)
		}
		clk.Add(time.Millisecond)
		warned := strings.Contains(c.compositor.Read().ConnectionMessage, "remote remains 100x50")
		if warned == ack {
			t.Fatalf("ack=%v warning=%q", ack, c.compositor.Read().ConnectionMessage)
		}
		c.toggleRestore()
		if g.request.Pending() != (terminal.WindowSize{}) {
			t.Fatal("restore did not cancel geometry request")
		}
		stop()
	}
}

func TestGeometryRechecksQueuedResizeAtDelivery(t *testing.T) {
	for _, restore := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		received := make(chan *protocolpb.Frame, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer ws.CloseNow()
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var frame protocolpb.Frame
			if proto.Unmarshal(data, &frame) == nil {
				received <- &frame
			}
		}))
		ws, _, err := websocket.Dial(ctx, server.URL, nil)
		if err != nil {
			cancel()
			server.Close()
			t.Fatal(err)
		}
		g, err := newClientGeometry(100, 50, 80, 24, false)
		if err != nil {
			t.Fatal(err)
		}
		c := &Client{Clock: clock.NewMock(), geometry: g, restoreMode: newRestoreMode(false), TermSize: func() (int, int) { return 80, 24 }}
		stale := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: 80, Rows: 24}}}
		c.writeMu.Lock()
		done := make(chan error, 1)
		go func() { done <- c.writeFrame(ctx, ws, stale) }()
		if restore {
			c.toggleRestore()
		}
		c.writeMu.Unlock()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if restore {
			if err := c.writeFrame(ctx, ws, c.buildHelloFrame(80, 24)); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case frame := <-received:
			if restore {
				if frame.GetResize() != nil || frame.GetHello() == nil || frame.GetHello().Cols != 0 || frame.GetHello().Rows != 0 {
					t.Fatalf("restore delivered geometry %v", frame)
				}
			} else if size := frame.GetResize(); size == nil || size.Cols != 100 || size.Rows != 50 || !size.Explicit {
				t.Fatalf("stale geometry delivered %v", frame)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if stale.GetResize().Cols != 80 || stale.GetResize().Rows != 24 || stale.GetResize().Explicit {
			t.Fatal("caller frame modified")
		}
		_ = ws.CloseNow()
		cancel()
		server.Close()
	}
}
