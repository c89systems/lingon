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

func TestAttachSubcommandAliasSupportsRestore(t *testing.T) {
	root := NewRootCommand(lingon.NewLoader())
	cmd, args, err := root.Find([]string{"a", "-r", "remote-session"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Name() != "attach" {
		t.Fatalf("alias resolved to %q", cmd.Name())
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	restore, err := cmd.Flags().GetBool("restore")
	if err != nil || !restore {
		t.Fatalf("restore: %v %v", restore, err)
	}
	if got := cmd.Flags().Args(); len(got) != 1 || got[0] != "remote-session" {
		t.Fatalf("arguments: %v", got)
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
