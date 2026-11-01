package attach

import (
	"bytes"
	"context"
	"io"
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
)

func TestRestoreRechecksQueuedFramesAtDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
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
	defer server.Close()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	c := &Client{Clock: clock.NewMock(), restoreMode: newRestoreMode(false)}
	hello := c.buildHelloFrame(80, 24)
	// Construct frames while normal sizing is active, then hold delivery behind
	// earlier input. No scheduler timing is needed: restoration changes before
	// the write lock is released.
	c.writeMu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- c.writeFrame(ctx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: 80, Rows: 24}}})
	}()
	c.toggleRestore()
	c.writeMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.writeFrame(ctx, ws, hello); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-received:
		if frame.GetHello() == nil || frame.GetHello().Cols != 0 || frame.GetHello().Rows != 0 {
			t.Fatalf("stale dimensions delivered: %v", frame)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if hello.GetHello().Cols != 80 {
		t.Fatal("shared caller frame was modified")
	}
}

func TestRestoreOmitsHelloDimensionsAndSuppressesResize(t *testing.T) {
	c := &Client{restoreMode: newRestoreMode(true)}
	hello := c.buildHelloFrame(80, 24).GetHello()
	if hello.Cols != 0 || hello.Rows != 0 {
		t.Fatalf("hello resizes remote: %v", hello)
	}
	if err := c.SendResize(context.Background(), 80, 24); err != nil {
		t.Fatal(err)
	}
	c.toggleRestore()
	hello = c.buildHelloFrame(80, 24).GetHello()
	if hello.Cols != 80 || hello.Rows != 24 {
		t.Fatalf("normal hello: %v", hello)
	}
	if err := c.SendResize(context.Background(), 80, 24); err == nil {
		t.Fatal("normal mode did not attempt delivery")
	}
}

func TestRestoreLoadingPlaceholderDoesNotRequestWindowSize(t *testing.T) {
	var output bytes.Buffer
	c := &Client{Clock: clock.NewMock(), Stdout: &output, restoreMode: newRestoreMode(true), TermSize: func() (int, int) { return 80, 24 }}
	c.renderSnapshot(mvu.BlankSnapshot(100, 30))
	if strings.Contains(output.String(), "\x1b[8;30;100t") {
		t.Fatal("placeholder was treated as the remote viewport")
	}
}

func TestRestoreRequestDeduplicationAndWarning(t *testing.T) {
	clk := clock.NewMock()
	var output bytes.Buffer
	cols, rows := 80, 24
	c := &Client{Clock: clk, Stdout: &output, restoreMode: newRestoreMode(true), TermSize: func() (int, int) { return cols, rows }}
	stop := c.startEffects(context.Background())
	defer stop()
	c.ensureCompositor()
	snap := mvu.BlankSnapshot(100, 30)
	c.restoreTerminalLocked(snap)
	c.restoreTerminalLocked(snap)
	if output.String() != "\x1b[8;30;100t" {
		t.Fatalf("requests %q", output.String())
	}
	clk.Add(999 * time.Millisecond)
	if strings.Contains(c.compositor.Read().ConnectionMessage, "remote remains") {
		t.Fatal("warning fired early")
	}
	clk.Add(time.Millisecond)
	if msg := c.compositor.Read().ConnectionMessage; !strings.Contains(msg, "remote remains 100x30") {
		t.Fatalf("warning %q", msg)
	}
	cols, rows = 100, 30
	c.restoreTerminalLocked(snap)
	output.Reset()
	cols = 90
	c.restoreTerminalLocked(snap)
	if output.String() != "\x1b[8;30;100t" {
		t.Fatal("manual resize not corrected")
	}
	c.toggleRestore()
	clk.Add(time.Second)
	if output.String() != "\x1b[8;30;100t" {
		t.Fatal("disabled mode emitted resize")
	}
}

func TestRestoreHiddenAndConfirmedRequestsDoNotWarn(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		clk := clock.NewMock()
		var output bytes.Buffer
		cols, rows := 80, 24
		c := &Client{Clock: clk, Stdout: &output, restoreMode: newRestoreMode(true), TermSize: func() (int, int) { return cols, rows }}
		stop := c.startEffects(context.Background())
		c.ensureCompositor()
		c.restoreTerminalLocked(mvu.BlankSnapshot(100, 30))
		if hidden {
			c.SetStdout(io.Discard)
		} else {
			cols, rows = 100, 30
		}
		clk.Add(time.Second)
		if msg := c.compositor.Read().ConnectionMessage; msg != "" {
			t.Fatalf("spurious warning: %s", msg)
		}
		stop()
	}
}
