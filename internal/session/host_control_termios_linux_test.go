//go:build linux

package session

import (
	"testing"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/publisher"
)

func TestControlOwnershipPreservesApplicationEOFSetting(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	local := &localSession{pty: master, holderID: publisher.HostControlID}
	// A child owns its terminal configuration while the host already owns input.
	if err := setVEOF(slave, 17); err != nil {
		t.Fatal(err)
	}
	local.setHolder(publisher.HostControlID)
	if got, err := getVEOF(slave); err != nil || got != 17 {
		t.Fatalf("redundant host ownership rewrote shell EOF: got %d, err %v; want 17", got, err)
	}
	local.setHolder("remote")
	if got, err := getVEOF(slave); err != nil || got != 17 {
		t.Fatalf("remote ownership rewrote application EOF: got %d, err %v; want 17", got, err)
	}
	local.setHolder(publisher.HostControlID)
	if got, err := getVEOF(slave); err != nil || got != 17 {
		t.Fatalf("host takeover rewrote application EOF: got %d, err %v; want 17", got, err)
	}
}
