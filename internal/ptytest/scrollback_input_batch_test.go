package ptytest_test

import (
	"io"
	"strings"
	"testing"

	"golang.org/x/term"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
)

// exactFirstInputRead makes read boundaries an explicit part of this regression.
// Its reader is the test-owned slave PTY, never the runner's terminal.
type exactFirstInputRead struct {
	io.ReadCloser
	size int
}

func (r *exactFirstInputRead) Read(p []byte) (int, error) {
	if r.size > 0 {
		n := r.size
		r.size = 0
		return io.ReadFull(r.ReadCloser, p[:n])
	}
	return r.ReadCloser.Read(p)
}

func TestMultiAttachReducesCoalescedScrollbackKeysBeforeNextBytePTY(t *testing.T) {
	for _, tc := range []struct {
		name, keys string
		exit       bool
		wheel      bool
	}{
		{"home", "\x0c[g", false, false},
		{"csi-home", "\x0c[\x1b[H", false, false},
		{"multiple-navigation", "\x0c[gGg", false, false},
		{"enter-navigate-exit-input", "\x0c[gGqclean\r", true, false},
		{"wheel-after-entry", "\x0c[\x1b[<64;1;1M", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) { testCoalescedScrollbackKeys(t, tc.keys, tc.exit, tc.wheel) })
	}
}

func testCoalescedScrollbackKeys(t *testing.T, keys string, exits, wheel bool) {
	t.Helper()
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "batch", SessionName: "batch", Shell: searchShell(t), Cols: 80, Rows: 16})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, recorder, "batch")
	master, slave := ptytest.OpenPTY(t, 80, 16)
	state, err := terminal.MakeRaw(slave)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = terminal.WithFileDescriptor(slave, func(fd uintptr) { _ = term.Restore(int(fd), state) }) })
	viewer := ptytest.NewPTYSessionWithClock(t, master, slave, 80, 16, clk)
	t.Cleanup(viewer.Cancel)
	created := make(chan *attach.Client, 1)
	client := &attach.MultiClient{
		Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: "batch",
		Insecure: true, RequestControl: true, Clock: clk,
		Stdin:  &exactFirstInputRead{ReadCloser: slave, size: len(keys)},
		Stdout: slave, Stderr: io.Discard,
		TermSize:            func() (int, int) { return 80, 16 },
		DisableSignalResize: true, DisableDesktopNotifications: true,
		OnView: func(_ string, c *attach.Client) { created <- c },
	}
	go func() { viewer.SetRunErr(client.Run(viewer.Context())) }()
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	// Produce history only after the view has connected. Its final live frame
	// then fences the history stream, including initial handshake replay.
	host.Send(strings.Repeat("history\r", 40) + "fence\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:fence") && strings.TrimSpace(s.Row(s.Rows-1)) == "READY>"
	})
	view := <-created
	snapshot := view.Snapshot()
	_, origin := render.ViewportOriginForSnapshot(snapshot, 80, 16)
	entryOffset := max(0, int(snapshot.Rows)-16-origin)
	// Ctrl+L [, then Home, delivered in exactly one Read. Home must be reduced
	// immediately after mode entry, without being passed to the application.
	viewer.Send(keys)
	if !exits {
		waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
			if wheel {
				return view.ScrollbackOffset() == entryOffset+3 && strings.Contains(s.Row(0), "%]")
			}
			return strings.Contains(s.Row(0), "[0%]")
		})
		viewer.Send("Gqclean\r")
	}
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:clean") })
	if viewer.Screen().Contains("REPLY:gclean") {
		t.Fatal("navigation leaked into application input")
	}
}
