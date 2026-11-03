package ptytest_test

import (
	"strings"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestBackgroundTabCannotClearActiveScrollbackIndicatorPTY(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(recorder))
	shell, emit := controlledScrollOutput(t)
	first := h.StartHost(ptytest.HostOptions{SessionID: "active", SessionName: "alpha", Shell: shell, Cols: 60, Rows: 12})
	t.Cleanup(first.Cancel)
	second := h.StartHost(ptytest.HostOptions{SessionID: "background", SessionName: "beta", Shell: searchShell(t), Cols: 60, Rows: 12})
	t.Cleanup(second.Cancel)
	waitSearchScreen(t, first, func(s ptytest.Screen) bool { return s.Contains("READY") })
	waitSearchScreen(t, second, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "active")
	waitRestoreHost(t, recorder, "background")
	emit(100)
	waitSearchScreen(t, first, func(s ptytest.Screen) bool { return s.Contains("ROW-00100") })
	var clients sync.Map
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "active", Cols: 60, Rows: 12, OnView: func(id string, c *attach.Client) { clients.Store(id, c) }})
	t.Cleanup(viewer.Cancel)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-00100") })
	viewer.Send("\x0cn")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") && !s.Contains("ROW-00100") })
	viewer.Send("\x0cp")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("ROW-00100") })
	viewer.Send("\x0c[")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[100%]") })
	a, ok := clients.Load("active")
	if !ok {
		t.Fatal("active view missing")
	}
	b, ok := clients.Load("background")
	if !ok {
		t.Fatal("background view missing")
	}
	active := a.(*attach.Client)
	background := b.(*attach.Client)
	before := active.Compositor().Read().ScrollbackMessage
	// The hidden client shares the actual UI model. Its synchronous refresh
	// must not clear the foreground's caption, even though output is discarded.
	background.RenderCurrent()
	if got := active.Compositor().Read().ScrollbackMessage; got != before {
		t.Fatalf("background refresh changed active caption %q -> %q", before, got)
	}
}
