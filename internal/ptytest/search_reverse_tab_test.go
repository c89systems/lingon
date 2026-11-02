package ptytest_test

import (
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/ptytest"
)

func TestSearchShiftTabCyclesBackwardHostAndAttachPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprintf("attach=%v", useAttach), func(t *testing.T) {
			terminal, settle := overflowSearchTerminal(t, useAttach)
			prefix := "*"
			if useAttach {
				prefix = ""
			}
			terminal.Send("\x0c/\t")
			searchField(t, terminal, "/ alpha")
			for i := 6; i >= 2; i-- {
				terminal.Send("\x1b[Z")
				want := fmt.Sprintf("[%salpha-%d]", prefix, i)
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), want) && s.Contains("REPLY:tab-1") })
				searchField(t, terminal, fmt.Sprintf("/ alpha-%d", i))
			}
			terminal.Send("\x1b[Z")
			searchField(t, terminal, "/ alpha")
			// Empty search begins at the last session when cycling backwards.
			terminal.Send("\x1b\x0c/\x1b[Z")
			searchField(t, terminal, "/ alpha-6")
			terminal.Send("\t")
			searchField(t, terminal, "/ alpha")
			terminal.DrainRaw()
			terminal.Send("\x1b[Z\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:tab-6") && !strings.HasPrefix(s.Row(s.Rows-1), "/ ")
			})
			if strings.Contains(terminal.DrainRaw(), "\x1b[2J") {
				t.Fatal("reverse search selection cleared the whole screen")
			}
			terminal.Send("clean\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:clean") && !s.Contains("[Z") })
		})
	}
}
