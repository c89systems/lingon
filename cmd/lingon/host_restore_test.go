package main

import (
	"strings"
	"testing"

	"pkt.systems/lingon"
)

func TestHostRestoreAndRespawnFlagsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		args             []string
		restore, respawn bool
	}{
		{nil, false, false}, {[]string{"-r"}, true, false}, {[]string{"--restore"}, true, false},
		{[]string{"-R"}, false, true}, {[]string{"--respawn"}, false, true}, {[]string{"-rR"}, true, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			cmd := NewRootCommand(lingon.NewLoader())
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			restore, err := cmd.Flags().GetBool("restore")
			if err != nil {
				t.Fatal(err)
			}
			respawn, err := cmd.Flags().GetBool("respawn")
			if err != nil {
				t.Fatal(err)
			}
			if restore != tc.restore || respawn != tc.respawn {
				t.Fatalf("restore=%v respawn=%v, want %v %v", restore, respawn, tc.restore, tc.respawn)
			}
		})
	}
}
