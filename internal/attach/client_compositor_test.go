package attach

import (
	"sync"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
)

func TestClientCompositorConcurrentStartupAndTabActivation(t *testing.T) {
	c := &Client{Clock: clock.NewMock()}
	owner := mvu.NewRuntime()
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			c.SetCompositor(owner)
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			if c.ensureCompositor() == nil {
				t.Error("startup has no compositor")
			}
		}
	}()
	close(start)
	workers.Wait()
	if c.Compositor() != owner {
		t.Fatal("tab activation lost the owner's compositor")
	}
}

func TestClientSetCompositorPreservesOwnerContext(t *testing.T) {
	ui := mvu.NewRuntime()
	ui.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{SessionID: "active-owner", Endpoint: "https://owner.example/v1"}})
	c := &Client{SessionID: "hidden-client", Endpoint: "https://client.example/v1"}
	c.SetCompositor(ui)
	if c.Compositor() != ui {
		t.Fatal("client did not retain shared compositor")
	}
	state := ui.Read()
	if state.SessionID != "active-owner" || state.Endpoint != "https://owner.example/v1" {
		t.Fatalf("client overwrote owner context: %+v", state)
	}
}

func TestClientCompositorEnsuresRuntime(t *testing.T) {
	c := &Client{}
	if c.Compositor() == nil {
		t.Fatalf("expected compositor to be initialized")
	}
}

func TestClientSetCompositorNilInitializesRuntime(t *testing.T) {
	c := &Client{
		Endpoint:  "https://relay.example/v1",
		SessionID: "session-a",
		Theme:     "default",
	}
	c.SetCompositor(nil)
	comp := c.Compositor()
	if comp == nil {
		t.Fatalf("expected compositor to be initialized when setting nil")
	}
	state := comp.State()
	if state.SessionID != "session-a" {
		t.Fatalf("expected session id propagated, got %q", state.SessionID)
	}
	if state.Endpoint != "https://relay.example/v1" {
		t.Fatalf("expected endpoint propagated, got %q", state.Endpoint)
	}
}
