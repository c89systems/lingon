package sessionmanage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestManagementCallDoesNotRetryAnUncertainResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/sessions/manage" || r.Header.Get("Authorization") != "Bearer owner" {
			t.Errorf("unexpected management request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"session_id":`))
	}))
	defer server.Close()
	_, err := Call(context.Background(), server.Client(), server.URL, "owner", Request{RequestID: "once", SessionID: "host", Kind: "create"})
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") || calls.Load() != 1 {
		t.Fatalf("calls=%d error=%v", calls.Load(), err)
	}
}

func TestCallNormalizesNamesBeforeSending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.SessionID != "stable" || req.Name != "build_server" {
			t.Errorf("sent %+v", req)
		}
		_ = json.NewEncoder(w).Encode(Result{SessionID: req.SessionID, Name: req.Name})
	}))
	defer server.Close()
	result, err := Call(context.Background(), server.Client(), server.URL, "owner", Request{RequestID: "normalize", SessionID: "stable", Kind: "rename", Name: "build server"})
	if err != nil || result.Name != "build_server" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestDirectoryCallAcceptsOneLevelListingLargerThanOldReplyLimit(t *testing.T) {
	directories := make([]string, 128)
	for i := range directories {
		directories[i] = "/owner/" + strings.Repeat("directory", 8) + fmt.Sprint(i) + "/"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Result{SessionID: "s", Path: "/owner", Directories: directories})
	}))
	defer server.Close()
	result, err := Call(t.Context(), server.Client(), server.URL, "owner", Request{RequestID: "list", SessionID: "s", Kind: "directory"})
	if err != nil || len(result.Directories) != 128 {
		t.Fatalf("listing: %d %v", len(result.Directories), err)
	}
}
