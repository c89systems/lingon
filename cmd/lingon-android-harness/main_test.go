package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHarnessReusesExplicitHostIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h, err := startHarness(ctx, harnessOptions{cleartext: true, basePath: "/v1", username: "reuse-test", password: "test-only", cols: 80, rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer h.stop()
	client := h.server.Client()
	for _, query := range []string{"session_id=../unsafe", "session_id=named&count=2"} {
		response, err := client.Post(h.endpoint+"/__harness/start-host?"+query, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid fixture accepted: %s", query)
		}
	}
	for i := 0; i < 2; i++ {
		// /bin/true is noninteractive; runner stdio is /dev/null and any PTY is
		// a fixture-owned child. This never accesses the runner's terminal.
		response, err := client.Post(h.endpoint+"/__harness/start-host?session_id=named&shell=/bin/true", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || len(body.IDs) != 1 || body.IDs[0] != "named" {
			t.Fatalf("named fixture: %+v status=%d error=%v", body, response.StatusCode, err)
		}
		if err := h.stopHost("named"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHarnessTransportModes(t *testing.T) {
	for _, cleartext := range []bool{false, true} {
		scheme := "https"
		if cleartext {
			scheme = "http"
		}
		t.Run(scheme, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h, err := startHarness(ctx, harnessOptions{cleartext: cleartext, basePath: "/v1", username: "transport-test", password: "test-only", cols: 80, rows: 24})
			if err != nil {
				t.Fatal(err)
			}
			defer h.stop()
			if !strings.HasPrefix(h.config.Endpoint, scheme+"://") {
				t.Fatalf("wrong endpoint: %s", h.config.Endpoint)
			}
			response, err := h.server.Client().Get(h.config.Endpoint + "/sessions")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("unexpected anonymous status: %d", response.StatusCode)
			}
		})
	}
}

func TestBuildHostSessionOptionsDisablesDesktopNotifications(t *testing.T) {
	t.Parallel()

	stdinFile, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open stdin: %v", err)
	}
	t.Cleanup(func() { _ = stdinFile.Close() })

	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open stdout: %v", err)
	}
	t.Cleanup(func() { _ = stdoutFile.Close() })

	var (
		ptyMu  sync.Mutex
		ptyBuf bytes.Buffer
	)
	opts := buildHostSessionOptions(
		"https://example.test/v1",
		"token",
		"host-1",
		"/bin/sh",
		80,
		24,
		"/tmp/lingon-harness/.lingon/tls",
		stdinFile,
		stdoutFile,
		false,
		&ptyMu,
		&ptyBuf,
	)
	if !opts.DisableDesktopNotifications {
		t.Fatal("expected android harness hosts to disable desktop notifications")
	}
	if opts.TLSDir != "/tmp/lingon-harness/.lingon/tls" {
		t.Fatalf("TLSDir = %q, want harness TLS dir", opts.TLSDir)
	}
}

func TestWriteHostScriptDoesNotTouchCallerTTY(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	scriptPath, err := writeHostScript(dir, "host-1", "/tmp/lingon-android-harness", "", 0)
	if err != nil {
		t.Fatalf("writeHostScript: %v", err)
	}
	if filepath.Dir(scriptPath) != dir {
		t.Fatalf("script path dir = %q, want %q", filepath.Dir(scriptPath), dir)
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "stty ") {
		t.Fatalf("expected host launcher script not to mutate tty settings, got:\n%s", content)
	}
	if strings.Contains(content, "/dev/tty") {
		t.Fatalf("expected host launcher script not to reference caller tty, got:\n%s", content)
	}
	if !strings.Contains(content, `exec "/tmp/lingon-android-harness" -host-echo -host-id "host-1"`) {
		t.Fatalf("expected host launcher script to exec harness host echo, got:\n%s", content)
	}
}

func TestHarnessStopRemovesTempRoot(t *testing.T) {
	t.Setenv("LINGON_CONFIG_DIR", "")
	t.Setenv("LINGON_TERMINAL_DISABLE_DESKTOP_NOTIFICATIONS", "")
	t.Setenv("LINGON_DEBUG_INPUT", "")
	t.Setenv("LINGON_HOST_PUBLISHER_PING_INTERVAL", "")
	t.Setenv("LINGON_HOST_PUBLISHER_PING_TIMEOUT", "")
	t.Setenv("LINGON_HOST_ECHO_LOG", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := startHarness(ctx, harnessOptions{
		sessionCount: 0,
		port:         0,
		basePath:     "/v1",
		username:     "testuser",
		password:     "testpass",
		cols:         80,
		rows:         24,
	})
	if err != nil {
		t.Fatalf("startHarness: %v", err)
	}

	baseDir := h.baseDir
	if filepath.Base(baseDir) == "" || !strings.HasPrefix(filepath.Base(baseDir), "lingon-android-harness-") {
		t.Fatalf("baseDir = %q, want lingon-android-harness temp dir", baseDir)
	}
	if _, err := os.Stat(baseDir); err != nil {
		t.Fatalf("expected harness temp dir to exist before stop: %v", err)
	}
	if !strings.HasPrefix(h.config.HostEchoLog, baseDir+string(os.PathSeparator)) {
		t.Fatalf("host echo log = %q, want path under %q", h.config.HostEchoLog, baseDir)
	}

	h.stop()
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		t.Fatalf("expected harness temp dir to be removed after stop, stat err=%v", err)
	}
}

func TestStopHostStopsOnlyRequestedSession(t *testing.T) {
	stopped := map[string]bool{}
	h := &harness{
		sessions: []sessionHandle{
			{id: "host-1", stop: func() { stopped["host-1"] = true }},
			{id: "host-2", stop: func() { stopped["host-2"] = true }},
		},
	}

	if err := h.stopHost("host-1"); err != nil {
		t.Fatalf("stopHost: %v", err)
	}
	if !stopped["host-1"] {
		t.Fatal("expected host-1 stop callback to run")
	}
	if stopped["host-2"] {
		t.Fatal("expected host-2 to keep running")
	}
	if len(h.sessions) != 1 || h.sessions[0].id != "host-2" {
		t.Fatalf("remaining sessions = %+v, want only host-2", h.sessions)
	}
	if err := h.stopHost("host-1"); err == nil {
		t.Fatal("expected stopping a missing host to fail")
	}
}
