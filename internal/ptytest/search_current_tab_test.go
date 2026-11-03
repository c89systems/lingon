package ptytest_test

import (
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/ptytest"
)

func TestEmptySearchCyclesFromCurrentSessionHostAndAttachPTY(t *testing.T) {
	for _, useAttach := range []bool{false, true} {
		t.Run(fmt.Sprintf("attach=%v", useAttach), func(t *testing.T) {
			viewer, settle := overflowSearchTerminal(t, useAttach)
			viewer.Send("\x0c/alpha-3\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-3") })
			for _, spec := range []struct{ key, candidate string }{{"\t", "alpha-4"}, {"\x1b[Z", "alpha-2"}} {
				viewer.Send("\x0c/" + spec.key)
				searchField(t, viewer, "/ "+spec.candidate)
				prefix := ""
				if !useAttach {
					prefix = "*"
				}
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return strings.Contains(s.Row(0), "["+prefix+spec.candidate+"]") && s.Contains("REPLY:tab-3")
				})
				viewer.Send("\x1bunchanged\r")
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
					return s.Contains("REPLY:unchanged") && s.Contains("REPLY:tab-3") && !strings.HasPrefix(s.Row(s.Rows-1), "/ ")
				})
			}
			viewer.Send("\x0c/\t\r")
			if useAttach {
				settle()
			}
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-4") && !s.Contains("REPLY:tab-3") })
		})
	}
}
