package ptytest_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func overflowSearchTerminal(t *testing.T, useAttach bool) (*ptytest.PTYSession, func()) {
	t.Helper()
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "overflow", SessionName: "alpha", Shell: searchShell(t), Cols: 100, Rows: 12})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	host.Send("tab-1\r")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
	for i := 2; i <= 6; i++ {
		host.Send("\x0cc")
		name := fmt.Sprintf("alpha-%d", i)
		waitSearchScreen(t, host, func(s ptytest.Screen) bool {
			return s.Contains(name) && s.Contains("READY>") && !s.Contains(fmt.Sprintf("REPLY:tab-%d", i-1))
		})
		host.Send(fmt.Sprintf("tab-%d\r", i))
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains(fmt.Sprintf("REPLY:tab-%d", i)) })
	}
	host.Send("\x0cn")
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
	if !useAttach {
		clk.Add(4 * time.Second)
		host.Resize(28, 12)
		host.Send("narrow\r")
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:narrow") })
		return host, func() { clk.Add(4 * time.Second) }
	}
	waitRestoreHost(t, recorder, "overflow")
	for i := 2; i <= 6; i++ {
		waitRestoreHost(t, recorder, fmt.Sprintf("alpha-%d", i))
	}
	ready := make(chan struct{}, 8)
	client := h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "overflow", Cols: 28, Rows: 12, OnView: func(_ string, c *attach.Client) {
		onReady := c.OnReady
		c.OnReady = func() { onReady(); ready <- struct{}{} }
	}})
	t.Cleanup(client.Cancel)
	waitSearchScreen(t, client, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-1") })
	settle := func() {
		t.Helper()
		select {
		case <-ready:
		case <-time.After(8 * time.Second):
			t.Fatal("view readiness barrier")
		}
		clk.Add(4 * time.Second)
	}
	settle()
	return client, settle
}

func TestTabOverflowFollowsActiveHostAndAttachPTY(t *testing.T) {
	for _, attach := range []bool{false, true} {
		t.Run(fmt.Sprintf("attach=%v", attach), func(t *testing.T) {
			terminal, settle := overflowSearchTerminal(t, attach)
			terminal.DrainRaw()
			terminal.Send("\x0cp")
			settle()
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return s.Contains("REPLY:tab-6") && strings.Contains(s.Row(0), "alpha-6") })
			if strings.Contains(terminal.DrainRaw(), "\x1b[2J") {
				t.Fatal("overflow tab switch cleared the whole screen")
			}
			terminal.Resize(18, 12)
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "alpha-6") })
			terminal.Resize(28, 12)
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "alpha-6") })
			terminal.DrainRaw()
			terminal.Send("\x0cn")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:tab-1") && strings.Contains(s.Row(0), "alpha") && !strings.Contains(s.Row(0), "alpha-6")
			})
			if strings.Contains(terminal.DrainRaw(), "\x1b[2J") {
				t.Fatal("overflow tab switch cleared the whole screen")
			}
		})
	}
}

func TestSearchHighlightsOverflowCandidateHostAndAttachPTY(t *testing.T) {
	for _, attach := range []bool{false, true} {
		t.Run(fmt.Sprintf("attach=%v", attach), func(t *testing.T) {
			terminal, settle := overflowSearchTerminal(t, attach)
			prefix := "*"
			if attach {
				prefix = ""
			}
			terminal.Send("\x0c/alpha-")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(0), "["+prefix+"alpha-2]") && s.Contains("REPLY:tab-1")
			})
			terminal.Send("6")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(0), "["+prefix+"alpha-6]") && s.Contains("REPLY:tab-1")
			})
			terminal.Send("\t")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return strings.Contains(s.Row(0), "["+prefix+"alpha]") && s.Contains("REPLY:tab-1")
			})
			// Cycle beyond the visible tabs without changing the active session.
			for i := 2; i <= 6; i++ {
				terminal.Send("\t")
				want := fmt.Sprintf("[%salpha-%d]", prefix, i)
				waitSearchScreen(t, terminal, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), want) && s.Contains("REPLY:tab-1") })
			}
			terminal.Send("\x1bcancelled\r")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:cancelled") && strings.Contains(s.Row(0), "alpha") && !strings.Contains(s.Row(0), "[") && !strings.Contains(s.Row(0), "alpha-6")
			})
			terminal.Send("\x0c/unknown")
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return strings.HasPrefix(s.Row(s.Rows-1), "/ unknown") && !strings.Contains(s.Row(0), "[") && strings.Contains(s.Row(0), "alpha")
			})
			terminal.Send("\x1b")
			terminal.Send("\x0c/alpha-6\r")
			settle()
			waitSearchScreen(t, terminal, func(s ptytest.Screen) bool {
				return s.Contains("REPLY:tab-6") && strings.Contains(s.Row(0), "alpha-6") && !strings.Contains(s.Row(0), "[")
			})
		})
	}
}
