package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pkt.systems/lingon/internal/sessionmanage"
)

func TestForceCloseRequiresSessionOwner(t *testing.T) {
	store := NewStore()
	server := NewHTTPServer(store, nil, nil, nil, NewHub(nil))
	store.CreateSession(Session{ID: "target", Username: "owner", Status: "active"})
	owner, _ := store.CreateAccessToken("owner", time.Hour, time.Now())
	other, _ := store.CreateAccessToken("other", time.Hour, time.Now())
	body, _ := json.Marshal(sessionmanage.Request{SessionID: "target", RequestID: "close", Kind: "close"})
	for _, tc := range []struct {
		token, share string
		status       int
	}{{"", "", 401}, {other.Token, "", 403}, {owner.Token, "true", 403}} {
		req := httptest.NewRequest(http.MethodPost, "/sessions/manage", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("X-Lingon-Share-Session", tc.share)
		reply := httptest.NewRecorder()
		server.Handler().ServeHTTP(reply, req)
		if reply.Code != tc.status {
			t.Fatalf("close authorization status=%d want=%d", reply.Code, tc.status)
		}
	}
}
