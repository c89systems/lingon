package attach_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestQueuedTextPasteUsesReadyApplicationModePTY(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	release := make(chan struct{})
	releaseSnapshot := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseSnapshot)
	inputs := make(chan []byte, 4)
	var handlers sync.WaitGroup
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var hello protocolpb.Frame
		if proto.Unmarshal(raw, &hello) != nil {
			return
		}
		send := func(f *protocolpb.Frame) bool {
			data, err := proto.Marshal(f)
			return err == nil && ws.Write(ctx, websocket.MessageBinary, data) == nil
		}
		if !send(&protocolpb.Frame{Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{HolderClientId: hello.GetHello().GetClientId()}}}) {
			return
		}
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		snap := mvu.BlankSnapshot(80, 20)
		snap.Mode = terminal.SnapshotModeBracketedPaste
		snap.Cursor = &protocolpb.Cursor{Y: 2}
		snap.CursorVisible = true
		if !send(&protocolpb.Frame{Seq: 1, Payload: &protocolpb.Frame_Snapshot{Snapshot: snap}}) {
			return
		}
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var f protocolpb.Frame
			if proto.Unmarshal(data, &f) != nil {
				return
			}
			if input := f.GetIn().GetData(); len(input) > 0 {
				select {
				case inputs <- input:
				case <-ctx.Done():
					return
				}
			}
		}
	}))
	t.Cleanup(func() { cancel(); server.Close(); handlers.Wait() })
	h := newHarness(t)
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{Endpoint: server.URL, SessionID: "queue", Cols: 80, Rows: 20,
		SessionSource: func(context.Context) ([]attach.SessionInfo, error) {
			return []attach.SessionInfo{{ID: "queue", Status: "running"}}, nil
		},
		OnView: func(_ string, c *attach.Client) { c.TextClipboard.Store("queued\n") },
	})
	t.Cleanup(viewer.Cancel)
	viewer.Send("\x0c]after\x0c/")
	fence, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	if err := viewer.WaitForScreen(fence, func(s ptytest.Screen) bool { return strings.HasPrefix(s.Row(s.Rows-1), "/ ") }); err != nil {
		t.Fatal("queued-input fence:", err)
	}
	releaseSnapshot()
	for _, want := range []string{"\x1b[200~queued\n\x1b[201~", "after"} {
		select {
		case got := <-inputs:
			if string(got) != want {
				t.Fatalf("ordered input=%q, want %q", got, want)
			}
		case <-fence.Done():
			t.Fatal("queued paste not delivered:", fence.Err())
		}
	}
}
