package mvu

import "testing"

func TestScrollbackAnchorProjectionIsPure(t *testing.T) {
	var view ScrollbackViewport
	view.EnterAt(20, 5, 3, 80, 40, 2)
	view.SyncHistory(ScrollbackPosition{End: 10})
	before := view
	for range 3 {
		if got := view.OffsetFor(ScrollbackPosition{End: 14}); got != 7 {
			t.Fatalf("offset=%d, want 7", got)
		}
		if view != before {
			t.Fatal("projection mutated the anchor")
		}
	}
	view.SyncHistory(ScrollbackPosition{End: 14})
	if view.OffsetFor(ScrollbackPosition{End: 14}) != 7 {
		t.Fatal("update advanced an already projected anchor twice")
	}
	if view.OffsetFor(ScrollbackPosition{End: 12}) != 7 {
		t.Fatal("stale history moved the anchor")
	}
	view.Exit()
	if view.OffsetFor(ScrollbackPosition{End: 100}) != 0 {
		t.Fatal("inactive view stopped following live output")
	}
}

func TestScrollbackUnchangedFrameWritesNothing(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			ui := NewRuntime()
			ui.ApplyAction(ScrollbackPercentAction{Visible: true, Percent: 50})
			var cache RenderCache
			snap := BlankSnapshot(40, 8)
			if mode == "host" {
				in := RuntimeHostFrameInput{Snapshot: snap, Cols: 40, Rows: 8, Cache: &cache}
				if _, err := ui.RenderHostFrame(in); err != nil {
					t.Fatal(err)
				}
				out, err := ui.RenderHostFrame(in)
				if err != nil {
					t.Fatal(err)
				}
				if len(out.Rendered.Bytes) != 0 {
					t.Fatalf("unchanged history emitted %q", out.Rendered.Bytes)
				}
			} else {
				in := RuntimeAttachFrameInput{Snapshot: snap, Cols: 40, Rows: 8, Cache: &cache, ScrollbackVisible: true}
				if _, err := ui.RenderAttachFrame(in); err != nil {
					t.Fatal(err)
				}
				out, err := ui.RenderAttachFrame(in)
				if err != nil {
					t.Fatal(err)
				}
				if len(out.Rendered.Bytes) != 0 {
					t.Fatalf("unchanged history emitted %q", out.Rendered.Bytes)
				}
			}
		})
	}
}
