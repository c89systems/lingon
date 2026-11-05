package ptytest_test

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/pslog"
)

type refreshEventWriter struct{ failed, success chan struct{} }

func (w *refreshEventWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("attach.sessions.refresh.failed")) {
		select {
		case w.failed <- struct{}{}:
		default:
		}
	}
	if bytes.Contains(p, []byte("attach.sessions.refresh.ok")) {
		select {
		case w.success <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}
func refreshBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatal("refresh processing barrier")
	}
}

func newRefreshProxyFixture(t *testing.T, ids ...string) *refreshProxyFixture {
	t.Helper()
	fixture := &refreshProxyFixture{}
	clk := clock.NewMock()
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
	if len(ids) == 0 {
		ids = []string{"auth-a", "auth-b"}
	}
	for _, id := range ids {
		host := h.StartHost(ptytest.HostOptions{SessionID: id, Shell: searchShell(t), Cols: 100, Rows: 20})
		t.Cleanup(host.Cancel)
		waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
		waitRestoreHost(t, recorder, id)
	}
	target, err := url.Parse(h.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	upstream := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Only the isolated fixture relay.
	t.Cleanup(upstream.CloseIdleConnections)
	forward := httputil.NewSingleHostReverseProxy(target)
	forward.Transport = upstream
	status, posts := &fixture.status, &fixture.posts
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/lingon")
		if r.URL.Path == "/auth/refresh" {
			posts.Add(1)
			if code := status.Load(); code != 0 {
				http.Error(w, "upstream graceful shutdown", int(code))
				return
			}
		}
		forward.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	endpoint := proxy.URL + "/lingon"
	authPath := filepath.Join(t.TempDir(), "auth.json")
	state := authstore.State{Endpoint: endpoint, AccessToken: h.AccessToken(), AccessExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), RefreshToken: h.RefreshToken().Token, RefreshExpiresAt: h.RefreshToken().ExpiresAt}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatal(err)
	}
	fixture.h, fixture.clk, fixture.endpoint, fixture.authPath, fixture.state = h, clk, endpoint, authPath, state
	fixture.recorder = recorder
	return fixture
}

type refreshProxyFixture struct {
	recorder           *ptytest.WSRecorder
	h                  *ptytest.Harness
	clk                *clock.MockClock
	endpoint, authPath string
	state              authstore.State
	status, posts      atomic.Int32
}

func TestProxyRefresh502PreservesAttachViewsPTY(t *testing.T) {
	fixture := newRefreshProxyFixture(t)
	h, clk, endpoint, authPath, state := fixture.h, fixture.clk, fixture.endpoint, fixture.authPath, fixture.state
	status, posts := &fixture.status, &fixture.posts
	events := &refreshEventWriter{failed: make(chan struct{}, 8), success: make(chan struct{}, 8)}
	ready := make(chan string, 8)
	var mu sync.Mutex
	views := map[string]*attach.Client{}
	viewer := h.StartMultiAttach(ptytest.MultiAttachOptions{Endpoint: endpoint, AuthFile: authPath, SessionID: "auth-a", Cols: 100, Rows: 20, InactiveTTL: time.Hour, RefreshInterval: time.Second, Logger: pslog.NewWithOptions(t.Context(), events, pslog.Options{Mode: pslog.ModeStructured}).LogLevel(pslog.DebugLevel), OnView: func(id string, c *attach.Client) {
		mu.Lock()
		views[id] = c
		mu.Unlock()
		onReady := c.OnReady
		c.OnReady = func() { onReady(); ready <- id }
	}})
	t.Cleanup(viewer.Cancel)
	waitReady := func(want string) {
		t.Helper()
		select {
		case id := <-ready:
			if id != want {
				t.Fatalf("ready %s want %s", id, want)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("view readiness barrier")
		}
	}
	waitReady("auth-a")
	viewer.Send("before-a\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:before-a") })
	viewer.Send("\x0cn")
	waitReady("auth-b")
	viewer.Send("before-b\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:before-b") })
	mu.Lock()
	firstA, firstB := views["auth-a"], views["auth-b"]
	mu.Unlock()
	state.AccessExpiresAt = time.Unix(1, 0)
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	status.Store(http.StatusBadGateway)
	clk.Add(time.Second)
	refreshBarrier(t, events.failed)
	if posts.Load() != 1 {
		t.Fatalf("refresh request replayed: %d POSTs", posts.Load())
	}
	after, err := os.ReadFile(authPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("transient failure changed saved credentials")
	}
	viewer.Send("after-502-b\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:after-502-b") })
	viewer.Send("\x0cpafter-502-a\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:after-502-a") })
	mu.Lock()
	same := views["auth-a"] == firstA && views["auth-b"] == firstB
	mu.Unlock()
	if !same {
		t.Fatal("transient refresh failure tore down healthy view connections")
	}
	// The next scheduled refresh recovers, without login or an immediate POST retry.
	status.Store(0)
	clk.Add(time.Second)
	refreshBarrier(t, events.success)
	updated, err := authstore.LoadForEndpoint(authPath, endpoint)
	if err != nil || updated.AccessToken == state.AccessToken || updated.RefreshToken != state.RefreshToken {
		t.Fatalf("refresh did not recover: error=%v", err)
	}
	if posts.Load() != 2 {
		t.Fatalf("unexpected refresh count: %d", posts.Load())
	}
	viewer.Send("recovered\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:recovered") })
}

func TestProxyRefresh502TabActivationKeepsCurrentViewPTY(t *testing.T) {
	fixture := newRefreshProxyFixture(t, "auth-a")
	ready := make(chan struct{}, 4)
	viewer := fixture.h.StartMultiAttach(ptytest.MultiAttachOptions{Endpoint: fixture.endpoint, AuthFile: fixture.authPath, SessionID: "auth-a", Cols: 100, Rows: 20, RefreshInterval: time.Hour, OnView: func(_ string, c *attach.Client) {
		onReady := c.OnReady
		c.OnReady = func() { onReady(); ready <- struct{}{} }
	}})
	t.Cleanup(viewer.Cancel)
	refreshBarrier(t, ready)
	viewer.Send("activation-before\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:activation-before") })
	fixture.state.AccessExpiresAt = time.Unix(1, 0)
	if err := authstore.Save(fixture.authPath, fixture.state); err != nil {
		t.Fatal(err)
	}
	fixture.status.Store(http.StatusBadGateway)
	// Introduce a tab after refresh starts failing, so no cached connection can
	// make this switch bypass the failing credential preparation.
	host := fixture.h.StartHost(ptytest.HostOptions{SessionID: "auth-b", Shell: searchShell(t), Cols: 100, Rows: 20})
	t.Cleanup(host.Cancel)
	waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
	waitRestoreHost(t, fixture.recorder, "auth-b")
	viewer.Send("\x0c/")
	searchField(t, viewer, "/")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "auth-b") })
	viewer.Send("auth-b")
	searchField(t, viewer, "/ auth-b")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(0), "[auth-b]") })
	viewer.Send("\x1b")
	before := fixture.posts.Load()
	// The next tab has no cached view. Auth refresh fails during its creation.
	// Following input is processed only after that activation attempt completes.
	viewer.Send("\x0cnafter-failed-switch\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:after-failed-switch") })
	if fixture.posts.Load() <= before {
		t.Fatalf("unexpected refresh attempts: %d", fixture.posts.Load())
	}
	fixture.status.Store(0)
	viewer.Send("\x0cnafter-switch-recovery\r")
	refreshBarrier(t, ready)
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:after-switch-recovery") && !s.Contains("REPLY:activation-before")
	})
	viewer.Send("\x0cpback-to-original\r")
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return s.Contains("REPLY:back-to-original") && s.Contains("REPLY:activation-before")
	})
}
