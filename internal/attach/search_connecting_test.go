package attach_test

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

type searchInputClock struct {
	*clock.MockClock
	inputTimer  chan struct{}
	escapeTimer chan struct{}
}

func (c *searchInputClock) NewTimer(d time.Duration) *clock.Timer {
	timer := c.MockClock.NewTimer(d)
	if d == 50*time.Millisecond {
		c.inputTimer <- struct{}{}
	}
	return timer
}

func (c *searchInputClock) AfterFunc(d time.Duration, f func()) *clock.Timer {
	timer := c.MockClock.AfterFunc(d, f)
	if d == 50*time.Millisecond {
		c.escapeTimer <- struct{}{}
	}
	return timer
}

func TestSearchWhileWaitingForFirstSnapshot(t *testing.T) {
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "pending.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		// Consume hello, then withhold all application frames until cleanup.
		_, _, _ = ws.Read(ctx)
		<-ctx.Done()
	})}
	served := make(chan struct{})
	go func() { defer close(served); _ = server.Serve(listener) }()
	t.Cleanup(func() { cancel(); _ = server.Close(); <-served })
	clk := &searchInputClock{MockClock: clock.NewMock(), inputTimer: make(chan struct{}, 1), escapeTimer: make(chan struct{}, 1)}
	h := ptytest.New(t, ptytest.WithClock(clk))
	terminal := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint: "local://headless", SessionID: "alpha", Cols: 80, Rows: 24, Restore: true,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "alpha", Name: "alpha"}, {ID: "beta", Name: "beta"}}, nil
		},
		SocketResolver: func(string) (string, error) { return listener.Addr().String(), nil },
	})
	t.Cleanup(terminal.Cancel)
	wait := func(check func(ptytest.Screen) bool) {
		t.Helper()
		bound, stop := context.WithTimeout(t.Context(), 3*time.Second)
		defer stop()
		if err := terminal.WaitForScreen(bound, check); err != nil {
			t.Fatalf("wait: %v\n%s", err, terminal.Screen().String())
		}
	}
	wait(func(s ptytest.Screen) bool { return s.Contains("loading local headless") })
	terminal.Send("\x0c/al")
	wait(func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(23)) == "/ alpha" })
	terminal.Send("\t\t")
	wait(func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(23)) == "/ beta" })
	terminal.Send("\x1b")
	select {
	case <-clk.inputTimer:
		clk.Add(50 * time.Millisecond)
	case <-time.After(3 * time.Second):
		t.Fatal("Escape parser did not arm its timer")
	}
	// Wait for search disambiguation to arm after the mouse filter flushes ESC.
	// Advance only once registration is proven; no scheduler-dependent sleeps.
	select {
	case <-clk.escapeTimer:
		clk.Add(50 * time.Millisecond)
	case <-time.After(3 * time.Second):
		t.Fatal("search Escape parser did not arm its timer")
	}
	wait(func(s ptytest.Screen) bool {
		return !strings.HasPrefix(s.Row(23), "/ ") && s.Contains("loading local headless")
	})
	if raw := terminal.DrainRaw(); strings.Contains(raw, "\x1b[8;24;80t") {
		t.Fatal("loading placeholder triggered window resize")
	}
}
