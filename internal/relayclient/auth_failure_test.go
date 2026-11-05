package relayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/authstore"
)

func TestRefreshFailureClassificationAndNoPostReplay(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/auth/refresh" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				http.Error(w, "proxy or auth failure", status)
			}))
			defer server.Close()
			_, err := RefreshToken(t.Context(), server.URL, "fixture-refresh")
			rejected := status == 401 || status == 403
			if err == nil || errors.Is(err, ErrCredentialsRejected) != rejected {
				t.Fatalf("status %d classified as %v", status, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("refresh POST replayed %d times", calls.Load())
			}
		})
	}
}

func TestTransientRefreshPreservesAuthFileAndRecovers(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusBadGateway)
	var calls atomic.Int32
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken != "refresh-original" {
			t.Errorf("refresh payload invalid: %v", err)
		}
		if status.Load() != 0 {
			http.Error(w, "Bad Gateway", int(status.Load()))
			return
		}
		_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: "access-new", AccessExpiresAt: future, RefreshToken: "refresh-original", RefreshExpiresAt: future})
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "auth.json")
	state := authstore.State{Endpoint: server.URL, AccessToken: "access-old", AccessExpiresAt: time.Unix(1, 0), RefreshToken: "refresh-original", RefreshExpiresAt: future}
	if err := authstore.Save(path, state); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := EnsureAccessToken(t.Context(), server.URL, path); err == nil || errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("502 treated as credential rejection: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed refresh changed stored credentials")
	}
	if calls.Load() != 1 {
		t.Fatal("request retried without a new caller operation")
	}
	status.Store(0)
	recovered, err := EnsureAccessToken(t.Context(), server.URL, path)
	if err != nil || recovered.AccessToken != "access-new" || recovered.RefreshToken != state.RefreshToken {
		t.Fatalf("recovery failed: %v", err)
	}
	stored, err := authstore.LoadForEndpoint(path, server.URL)
	if err != nil || stored.AccessToken != "access-new" {
		t.Fatalf("recovered token was not saved: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected number of explicit attempts: %d", calls.Load())
	}
}

func TestMalformedRefreshAndCancellationAreNotCredentialRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not a token response</html>"))
	}))
	defer server.Close()
	if _, err := RefreshToken(t.Context(), server.URL, "fixture"); err == nil || errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("malformed success: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RefreshToken(ctx, server.URL, "fixture"); !errors.Is(err, context.Canceled) || errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("cancellation: %v", err)
	}
}
