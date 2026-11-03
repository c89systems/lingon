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
	"pkt.systems/lingon/internal/terminal"
)

func TestRestoreGeometryWaitsForRemoteGridAndDoesNotLeakToOtherTabs(t *testing.T) {
	g, err := newClientGeometry(100, 50, 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	g.bindInitialSession("alpha")
	var output bytes.Buffer
	c := &Client{SessionID: "alpha", geometry: g, restoreMode: newRestoreMode(true), Clock: clock.NewMock(), Stdout: &output, TermSize: func() (int, int) { return 80, 24 }}
	stop := c.startEffects(t.Context())
	defer stop()
	var other bytes.Buffer
	b := &Client{SessionID: "beta", geometry: g, restoreMode: c.restoreMode, Clock: c.Clock, Stdout: &other, TermSize: c.TermSize}
	stopOther := b.startEffects(t.Context())
	defer stopOther()
	// Switching while the initial connection is pending must neither move the
	// startup override to beta nor block beta from restoring its own grid.
	if err := b.applyGeometry(t.Context()); err != nil {
		t.Fatal("pending startup geometry leaked into another tab", err)
	}
	b.restoreTerminalLocked(mvu.BlankSnapshot(95, 28))
	if other.String() != "\x1b[8;28;95t" {
		t.Fatal("pending startup geometry blocked the other session")
	}
	for _, size := range []terminal.WindowSize{{Cols: 90, Rows: 30}, {Cols: 99, Rows: 50}} {
		c.restoreTerminalLocked(mvu.BlankSnapshot(size.Cols, size.Rows))
		if output.Len() != 0 {
			t.Fatal("window restored before remote acknowledged startup geometry")
		}
	}
	c.restoreTerminalLocked(mvu.BlankSnapshot(100, 50))
	if output.String() != "\x1b[8;50;100t" {
		t.Fatalf("acknowledged geometry not restored: %q", output.String())
	}
	if err := c.applyGeometry(t.Context()); err != nil {
		t.Fatal("completed geometry was retried", err)
	}
	if err := b.applyGeometry(t.Context()); err != nil {
		t.Fatal("startup geometry leaked into another tab", err)
	}
	b.restoreTerminalLocked(mvu.BlankSnapshot(95, 28))
	if other.String() != "\x1b[8;28;95t" {
		t.Fatal("other session lost its dimensions")
	}
	c.SetStdout(io.Discard)
	c.SetStdout(&output)
	output.Reset()
	c.restoreTerminalLocked(mvu.BlankSnapshot(88, 26))
	if output.String() != "\x1b[8;26;88t" {
		t.Fatal("returning to original tab reapplied startup geometry")
	}
}

func TestRestoreGeometryDenialRestoresActualRemoteSize(t *testing.T) {
	g, err := newClientGeometry(100, 50, 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	g.bindInitialSession("alpha")
	var output bytes.Buffer
	c := &Client{SessionID: "alpha", geometry: g, restoreMode: newRestoreMode(true), Clock: clock.NewMock(), Stdout: &output, viewOnly: true, TermSize: func() (int, int) { return 80, 24 }}
	stop := c.startEffects(t.Context())
	defer stop()
	if err := c.applyGeometry(t.Context()); err != nil {
		t.Fatal("read-only startup attempted a resize", err)
	}
	c.restoreTerminalLocked(mvu.BlankSnapshot(90, 30))
	if output.String() != "\x1b[8;30;90t" {
		t.Fatal("rejected geometry replaced the actual remote size")
	}
}

func TestRestoreGeometryAllowsManualResizeBeforeStartupAcknowledgement(t *testing.T) {
	g, err := newClientGeometry(100, 50, 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	g.bindInitialSession("alpha")
	var output bytes.Buffer
	c := &Client{SessionID: "alpha", geometry: g, restoreMode: newRestoreMode(true), restoreLocal: terminal.WindowSize{Cols: 80, Rows: 24}, Clock: clock.NewMock(), Stdout: &output, TermSize: func() (int, int) { return 95, 25 }}
	stop := c.startEffects(t.Context())
	defer stop()
	if err := c.processTerminalResize(t.Context(), false); err == nil {
		t.Fatal("expected disconnected manual delivery")
	}
	if c.restoreGeometryPending(terminal.WindowSize{Cols: 90, Rows: 30}) {
		t.Fatal("startup geometry overrode a newer manual resize")
	}
	if err := c.applyGeometry(t.Context()); err != nil {
		t.Fatal("superseded startup geometry was retried", err)
	}
	if strings.Contains(output.String(), "\x1b[8;50;100t") {
		t.Fatal("superseded geometry resized the window")
	}
}

func TestRestoreGeometryRechecksQueuedStartupSizeAtDelivery(t *testing.T) {
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
	g, err := newClientGeometry(100, 50, 80, 24, true)
	if err != nil {
		t.Fatal(err)
	}
	g.bindInitialSession("alpha")
	c := &Client{SessionID: "alpha", geometry: g, restoreMode: newRestoreMode(true), Clock: clock.NewMock()}
	c.setWebSocket(ws)
	c.writeMu.Lock()
	done := make(chan error, 1)
	go func() { done <- c.applyGeometry(ctx) }()
	c.cancelRestoreGeometry()
	c.restoreState()
	c.restoreManual.Set(terminal.WindowSize{Cols: 88, Rows: 26})
	c.writeMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.applyRestoreManualResize(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-received:
		resize := frame.GetResize()
		if resize == nil || resize.Cols != 88 || resize.Rows != 26 || !resize.Explicit {
			t.Fatalf("stale startup geometry reached remote: %v", frame)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
