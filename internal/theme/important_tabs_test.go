package theme

import (
	"fmt"
	"testing"
)

func TestImportantTabsDimThemeColors(t *testing.T) {
	color := func(sequence string) Color {
		t.Helper()
		var kind int
		var c Color
		if n, err := fmt.Sscanf(sequence, "\x1b[%d;2;%d;%d;%dm", &kind, &c.R, &c.G, &c.B); n != 4 || err != nil {
			t.Fatalf("invalid theme color %q: %v", sequence, err)
		}
		return c
	}
	for _, name := range Names() {
		ui := TUI(name)
		for _, pair := range [][2]string{{ui.TabBg, ui.DimmedTabs.Bg}, {ui.TabActiveBg, ui.DimmedTabs.ActiveBg}} {
			if luminance(color(pair[1])) >= luminance(color(pair[0])) {
				t.Fatalf("%s: important-mode background/foreground is not darker", name)
			}
		}
		for _, pair := range [][2]string{{ui.DimmedTabs.Fg, ui.DimmedTabs.Bg}, {ui.DimmedTabs.ActiveFg, ui.DimmedTabs.ActiveBg}, {ui.DimmedTabs.MutedFg, ui.DimmedTabs.Bg}, {ui.DimmedTabs.MutedActiveFg, ui.DimmedTabs.ActiveBg}} {
			if contrastRatio(color(pair[0]), color(pair[1])) < 3 {
				t.Fatalf("%s: dimmed tab text is unreadable", name)
			}
		}
		if ui.DimmedTabs.Bg == ui.DimmedTabs.ActiveBg {
			t.Fatalf("%s: dimming removed active-tab distinction", name)
		}
	}
}
