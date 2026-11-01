package relay

import (
	"context"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestReusedSessionIDDoesNotReplayPreviousHost(t *testing.T) {
	for _, explicitClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnected", true: "closed"}[explicitClose], func(t *testing.T) {
			ctx := context.Background()
			hub := NewHub(nil)
			old := &fakeConn{id: "old", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
			_ = hub.RegisterHost(old, "s1", 80, 24)
			_ = hub.HandleHostFrame(ctx, old, hostSnapshotFrame("old contents: exit"))
			cachedSeq := hub.session("s1").seq
			if explicitClose {
				err := hub.HandleHostFrame(ctx, old, &protocolpb.Frame{Payload: &protocolpb.Frame_SessionClosed{SessionClosed: &protocolpb.SessionClosed{Reason: "exited"}}})
				if err != errHostSessionClosed {
					t.Fatal(err)
				}
			} else {
				_ = hub.HandleHostFrame(ctx, old, hostDiffFrame("old contents: exit"))
				cachedSeq = hub.session("s1").seq
			}
			hub.Unregister(old)
			replacement := &fakeConn{id: "new", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
			_ = hub.RegisterHost(replacement, "s1", 80, 24)
			// The replacement may publish before the old client reconnects. A
			// cursor from the previous host still cannot establish a new baseline.
			if err := hub.HandleHostFrame(ctx, replacement, hostSnapshotFrame("new contents")); err != nil {
				t.Fatal(err)
			}
			client := &fakeConn{id: "android", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
			hub.RegisterClient(client, "s1", "android", false)
			hello := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "android", LastSeq: cachedSeq}}}
			if err := hub.HandleClientFrame(ctx, client, hello); err != nil {
				t.Fatal(err)
			}
			if len(client.sent) != 0 {
				t.Fatalf("old cursor reused without requesting replacement scrollback baseline: %v", client.sent)
			}
			if len(replacement.sent) != 1 || replacement.sent[0].GetHello() == nil {
				t.Fatal("replacement host was not asked for fresh content")
			}
			if err := hub.HandleHostFrame(ctx, replacement, hostSnapshotFrame("new contents")); err != nil {
				t.Fatal(err)
			}
			got := client.sent[len(client.sent)-1]
			if got.GetSnapshot().GetTitle() != "new contents" || got.Seq <= cachedSeq {
				t.Fatalf("replacement snapshot: %v", got)
			}
		})
	}
}
