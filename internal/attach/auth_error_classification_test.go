package attach

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/relayclient"
)

func TestRefreshErrorOnlyExpiresRejectedCredentials(t *testing.T) {
	for _, tc := range []struct {
		err     error
		expired bool
	}{
		{fmt.Errorf("refresh: %w", relayclient.ErrCredentialsRejected), true},
		{ErrAuthExpired, true}, {fmt.Errorf("refresh failed: 502 Bad Gateway"), false},
		{context.DeadlineExceeded, false}, {context.Canceled, false},
	} {
		err := authRefreshError("https://fixture.invalid/lingon", tc.err)
		if errors.Is(err, ErrAuthExpired) != tc.expired {
			t.Fatalf("%v classified as %v", tc.err, err)
		}
		if !tc.expired && strings.Contains(err.Error(), "lingon login") {
			t.Fatal("transient failure told operator to log in again")
		}
		if !tc.expired && !errors.Is(err, tc.err) {
			t.Fatal("underlying transport/context error lost")
		}
	}
}
