//go:build integration

package integrationptyattach_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/sessionmanage"
)

func popupCreationIntent(t *testing.T, closePopup bool) {
	release := make(chan struct{})
	releaseCreation := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseCreation)
	entered := make(chan sessionmanage.Request, 1)
	recorder := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(recorder), ptytest.WithRequestHook(func(r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sessions/manage") {
			return
		}
		data, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		r.Body = io.NopCloser(bytes.NewReader(data))
		var req sessionmanage.Request
		if json.Unmarshal(data, &req) == nil && req.Kind == "create" {
			entered <- req
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
	}))
	home := shortHomeDir(t)
	cfg := filepath.Join(home, lingon.DefaultConfigDirName)
	cleanupManagedTerminals(t, cfg)
	const parent = "pending-popup-source"
	cmd := exec.Command(buildLingonAttachBinary(t), "-x", "--session", parent, "--cols", "100", "--rows", "20", "--shell", fixedPromptEmitRowsBash(t),
		"--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure")
	cmd.Env = testAttachCLIEnv(home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("start source: %v %s", err, out)
	}
	waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{parent}, 8*time.Second)
	viewer := startLingonAttachCLI(t, h, parent, 100, 20)
	t.Cleanup(viewer.Cancel)
	waitForPromptVisible(t, viewer, 8*time.Second)
	viewer.Send("\x0cj")
	var req sessionmanage.Request
	select {
	case req = <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("popup create request never entered the server")
	}
	// Search is an observable fence for all preceding input while creation is held.
	if closePopup {
		viewer.Send("\x0cJ\x0c/")
	} else {
		viewer.Send("printf 'QUEUED_%s\\n' POPUP\r\x0c/")
	}
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.HasPrefix(s.Row(s.Rows-1), "/ ") })
	if !closePopup {
		// Fence the parent's ordered input stream. This proves absence of the
		// earlier command on that stream without waiting for a quiet interval.
		viewer.Send("\x1b\x0cjprintf 'PARENT_%s\\n' FENCED\r")
		popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_FENCED") })
		if viewer.Screen().Contains("QUEUED_POPUP") {
			releaseCreation()
			t.Fatal("input intended for the pending popup was executed in its parent")
		}
	}
	releaseCreation()
	if !closePopup {
		viewer.Send("\x0cj")
		popupScreen(t, viewer, func(s ptytest.Screen) bool {
			return strings.Contains(strings.Join(s.Lines[11:], "\n"), "QUEUED_POPUP") && s.Contains("j hide")
		})
		viewer.Send("\x0cj")
		popupScreen(t, viewer, func(s ptytest.Screen) bool { return !s.Contains("j hide") && !s.Contains("QUEUED_POPUP") })
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	child := sessionmanage.CreatedSessionID(req)
	if err := recorder.WaitForFrame(ctx, func(record ptytest.FrameRecord) bool {
		var frame protocolpb.Frame
		return record.SessionID == child && proto.Unmarshal(record.Raw, &frame) == nil && frame.GetSessionClosed() != nil
	}); err != nil {
		t.Fatalf("queued explicit popup close was lost: %v", err)
	}
	viewer.Send("\x1bprintf 'PARENT_%s\\n' SURVIVES\r")
	popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("PARENT_SURVIVES") && !s.Contains("j hide") })
}

func TestPopupCreationProcessesQueuedIntentCLI(t *testing.T) {
	t.Run("input", func(t *testing.T) { popupCreationIntent(t, false) })
	t.Run("close", func(t *testing.T) { popupCreationIntent(t, true) })
}
