package main

import (
	"testing"

	"pkt.systems/lingon"
)

func TestAttachRestoreFlagAliases(t *testing.T) {
	for _, flag := range []string{"-r", "--restore"} {
		cmd := NewAttachCommand(lingon.NewLoader())
		if err := cmd.Flags().Parse([]string{flag, "session"}); err != nil {
			t.Fatal(err)
		}
		enabled, err := cmd.Flags().GetBool("restore")
		if err != nil || !enabled {
			t.Fatalf("%s: %v %v", flag, enabled, err)
		}
	}
}

func TestAttachHostnameOnlyFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewAttachCommand(loader)

	if err := cmd.Flags().Parse([]string{"--hostname-only"}); err != nil {
		t.Fatalf("parse --hostname-only: %v", err)
	}
	hostnameOnly, err := cmd.Flags().GetBool("hostname-only")
	if err != nil {
		t.Fatalf("get hostname-only flag: %v", err)
	}
	if !hostnameOnly {
		t.Fatalf("expected --hostname-only to set flag")
	}
}
