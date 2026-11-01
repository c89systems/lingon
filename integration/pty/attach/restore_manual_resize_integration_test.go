//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRestoreCLIManualSIGWINCHRemainsResponsive(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithClock(clock.New()), ptytest.WithWSRecorder(recorder))
	host := h.StartHost(ptytest.HostOptions{SessionID: "manual-restore", Cols: 100, Rows: 30, Shell: fixedPromptEmitRowsBash(t)})
	t.Cleanup(host.Cancel)
	waitForPromptVisible(t, host, 8*time.Second)
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"manual-restore"}, 8*time.Second)
	terminal := startGeometryCLI(t, shortHomeDir(t), "a", "manual-restore", "-r", "--endpoint", h.Endpoint(), "--access-token", h.AccessToken(), "--insecure", "--request-control", "--disable-desktop-notifications")
	t.Cleanup(terminal.Cancel)
	wait := func(check func(ptytest.Screen) bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		if err := terminal.WaitForScreen(ctx, check); err != nil {
			t.Fatalf("%v\n%s", err, terminal.Screen().String())
		}
	}
	var raw string
	wait(func(_ ptytest.Screen) bool {
		raw += terminal.DrainRaw()
		return strings.Contains(raw, "\x1b[8;30;100t")
	})
	// The CLI is the foreground group of this test-owned child PTY. Its kernel
	// SIGWINCH is isolated from the runner and exercises the real signal path.
	terminal.Resize(100, 30)
	terminal.Send("printf 'ACK:%s\\n' \"$(stty size)\"\r")
	wait(func(s ptytest.Screen) bool { return s.Contains("ACK:30 100") })
	windowOperation := regexp.MustCompile("\x1b\\[8;[0-9]+;[0-9]+t")
	for i, size := range []struct{ cols, rows int }{{92, 29}, {78, 20}, {104, 33}, {88, 26}} {
		terminal.DrainRaw()
		terminal.Resize(size.cols, size.rows)
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		err := recorder.WaitForFrame(ctx, func(r ptytest.FrameRecord) bool {
			if r.Role != "host" || r.Direction != ptytest.DirClientToServer || r.SessionID != "manual-restore" {
				return false
			}
			var frame protocolpb.Frame
			return proto.Unmarshal(r.Raw, &frame) == nil && int(frame.GetSnapshot().GetCols()) == size.cols && int(frame.GetSnapshot().GetRows()) == size.rows
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		terminal.Send(fmt.Sprintf("printf 'LIVE_%d:%%s\\n' \"$(stty size)\"\r", i))
		want := fmt.Sprintf("LIVE_%d:%d %d", i, size.rows, size.cols)
		wait(func(s ptytest.Screen) bool { return s.Contains(want) })
		if raw := terminal.DrainRaw(); windowOperation.MatchString(raw) {
			t.Fatalf("manual resize reissued window operation: %q", raw)
		}
	}
	terminal.Send("\x0c0printf 'AFTER_REDRAW:%s\\n' \"$(stty size)\"\r")
	wait(func(s ptytest.Screen) bool { return s.Contains("AFTER_REDRAW:26 88") })
}
