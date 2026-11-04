package attach_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachNewCursorFrameOverridesTabSwitchWakePTY(t *testing.T) {
	for _, update := range []string{"snapshot", "diff"} {
		for _, withStatus := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/status=%v", update, withStatus), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				var handlers sync.WaitGroup
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/sessions") {
						_ = json.NewEncoder(w).Encode([]map[string]string{{"id": "first", "status": "running"}, {"id": "second", "status": "running"}})
						return
					}
					handlers.Add(1)
					defer handlers.Done()
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer conn.CloseNow()
					read := func() *protocolpb.Frame {
						_, data, err := conn.Read(ctx)
						if err != nil {
							return nil
						}
						var frame protocolpb.Frame
						if proto.Unmarshal(data, &frame) != nil {
							return nil
						}
						return &frame
					}
					send := func(frame *protocolpb.Frame) bool {
						data, err := proto.Marshal(frame)
						return err == nil && conn.Write(ctx, websocket.MessageBinary, data) == nil
					}
					hello := read()
					if hello == nil {
						return
					}
					id := hello.GetSessionId()
					if id == "" {
						send(&protocolpb.Frame{Payload: &protocolpb.Frame_Sessions{Sessions: &protocolpb.Sessions{Sessions: []*protocolpb.SessionInfo{{Id: "first", Status: "running"}, {Id: "second", Status: "running"}}}}})
						for read() != nil {
						}
						return
					}
					snap := mvu.BlankSnapshot(100, 20)
					snap.Cursor, snap.CursorVisible = &protocolpb.Cursor{X: 7, Y: 3}, true
					for i, ch := range "VIEW_" + id {
						snap.Runes[3*100+i] = uint32(ch)
					}
					if !send(&protocolpb.Frame{Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{HolderClientId: hello.GetHello().GetClientId()}}}) ||
						!send(&protocolpb.Frame{Seq: 1, Payload: &protocolpb.Frame_Snapshot{Snapshot: snap}}) {
						return
					}
					for frame := read(); frame != nil; frame = read() {
						if len(frame.GetIn().GetData()) == 0 {
							continue
						}
						// One application frame moves the cursor to row 1, then stays idle.
						// No timer, further output, or additional key can repair the screen.
						top := mvu.BlankSnapshot(100, 20)
						top.Cursor, top.CursorVisible = &protocolpb.Cursor{X: 7}, true
						for i, ch := range "PROMPT>" {
							top.Runes[i] = uint32(ch)
						}
						next := &protocolpb.Frame{Seq: 2, Payload: &protocolpb.Frame_Snapshot{Snapshot: top}}
						if update == "diff" {
							diff := &protocolpb.Diff{Cols: 100, Rows: 20, Cursor: top.Cursor, CursorVisible: true}
							for row := 0; row < 20; row++ {
								diff.DiffRows = append(diff.DiffRows, &protocolpb.DiffRow{Row: uint32(row), Runes: top.Runes[row*100 : (row+1)*100]})
							}
							next.Payload = &protocolpb.Frame_Diff{Diff: diff}
						}
						if !send(next) {
							return
						}
					}
				}))
				t.Cleanup(func() { cancel(); server.Close(); handlers.Wait() })
				clk := clock.NewMock()
				h := newHarness(t, ptytest.WithClock(clk))
				viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{Endpoint: server.URL, AccessToken: "test", SessionID: "first", Cols: 100, Rows: 20})
				t.Cleanup(viewer.Cancel)
				waitTabSwitchFrame(t, viewer, "VIEW_first", "VIEW_second")
				for _, id := range []string{"second", "first"} {
					viewer.Send("\x0cn")
					waitTabSwitchFrame(t, viewer, "VIEW_"+id, "PROMPT>")
				}
				if !withStatus {
					clk.Add(4 * time.Second)
					deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
					defer stop()
					if err := viewer.WaitForScreen(deadline, func(s ptytest.Screen) bool {
						return s.Contains("VIEW_first") && !s.Contains("connected to")
					}); err != nil {
						t.Fatal("status expiry barrier:", err)
					}
				}
				viewer.Send("\x0cn")
				waitTabSwitchFrame(t, viewer, "VIEW_second", "VIEW_first")
				if update == "snapshot" {
					viewer.Send("\x0c\x0c")
				} else {
					// The same rule applies when unrelated application input moves it.
					viewer.Send("x")
				}
				deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
				defer stop()
				if err := viewer.WaitForScreen(deadline, func(s ptytest.Screen) bool {
					return strings.HasPrefix(s.Row(0), "PROMPT>") && viewer.Cursor().Row == 1
				}); err != nil {
					t.Fatalf("new row-1 cursor did not hide switched tab bar: %v\n%s", err, viewer.Screen().String())
				}
			})
		}
	}
}
