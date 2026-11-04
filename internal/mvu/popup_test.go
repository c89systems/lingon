package mvu

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/theme"
)

func TestPopupGeometryCompositionAndTheme(t *testing.T) {
	for rows, want := range map[int]int{1: 0, 2: 0, 3: 0, 4: 1, 12: 5, 47: 22, 48: 23, 80: 23} {
		if got := PopupRows(rows); got != want {
			t.Fatalf("viewport rows=%d: terminal rows=%d, want %d plus one header row", rows, got, want)
		}
	}
	for _, rows := range []int{1, 2, 3, 4, 12, 47, 48, 80} {
		const cols = 100
		base := BlankSnapshot(cols, rows)
		for i := range base.Runes {
			base.Runes[i] = 'B'
		}
		original := proto.Clone(base)
		height := PopupRows(rows)
		pane := BlankSnapshot(cols, max(1, height))
		for i := range pane.Runes {
			pane.Runes[i] = 'P'
		}
		pane.Cursor = &protocolpb.Cursor{X: 7, Y: uint32(max(0, height-1))}
		pane.CursorVisible = true
		var previous *protocolpb.Snapshot
		for _, name := range []string{"default", "doom-nord"} {
			state := State{Theme: theme.TUI(name), Popup: &Popup{Name: "tools", Snapshot: pane}}
			out, err := RenderHost(HostRenderInput{Snapshot: base, Cols: cols, Rows: rows, State: state})
			if err != nil {
				t.Fatal(err)
			}
			frame := out.ComposedSnapshot
			if !proto.Equal(base, original) {
				t.Fatal("composition mutated the parent terminal")
			}
			if height == 0 {
				continue
			}
			bar := rows - height - 1
			if got := string(runesFromProto(frame.Runes[bar*cols : (bar+1)*cols])); !strings.Contains(got, "tools  Ctrl+L: j hide · J close · h help") {
				t.Fatalf("rows=%d bar=%q", rows, got)
			}
			if frame.Runes[(bar+1)*cols] != 'P' || (bar > 0 && frame.Runes[(bar-1)*cols] != 'B') {
				t.Fatal("popup did not cover only its rectangle")
			}
			if frame.Cursor.GetY() != uint32(rows-1) || frame.Cursor.GetX() != 7 || !frame.CursorVisible {
				t.Fatalf("popup cursor: %v", frame.Cursor)
			}
			if previous != nil && previous.Bg[bar*cols] == frame.Bg[bar*cols] && previous.Fg[bar*cols] == frame.Fg[bar*cols] {
				t.Fatal("header ignored theme selection")
			}
			previous = frame
		}
	}
}

func runesFromProto(in []uint32) []rune {
	out := make([]rune, len(in))
	for i, r := range in {
		out[i] = rune(r)
	}
	return out
}

func TestPopupAssociationsHideAndPromoteWithoutTerminating(t *testing.T) {
	var p Popups
	p.Add("one", Popup{SessionID: "a"})
	p.Add("two", Popup{SessionID: "b"})
	if p.Focus("one") != "a" || p.Focus("two") != "b" {
		t.Fatal("popup focus crossed parent tabs")
	}
	p.Toggle("one")
	if p.Frame("one") != nil || p.ID("one") != "a" || p.Focus("one") != "one" {
		t.Fatal("hide discarded or retained focus on the popup")
	}
	p.Toggle("one")
	if p.Frame("one").SessionID != "a" {
		t.Fatal("show replaced the popup")
	}
	p.Remove("one")
	if p.Parent("a") != "" || p.Parent("b") != "two" {
		t.Fatal("closing a parent failed to promote only its child")
	}
	p.Remove("b")
	if p.Frame("two") != nil || p.Focus("two") != "two" {
		t.Fatal("closing a popup failed to return focus")
	}
}
