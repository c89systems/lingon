package control

import "testing"

func TestPrefixControlAliases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		key    byte
		letter byte
		want   Action
	}{
		{"popup", 0x0a, 'j', ActionPopup},
		{"next", 0x0e, 'n', ActionNextTab},
		{"previous", 0x10, 'p', ActionPrevTab},
		{"theme", 0x14, 't', ActionNextTheme},
		{"paste-text", ']', ']', ActionPasteText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []byte{tc.letter, tc.key} {
				var p Prefix
				if action, out := p.Feed(CtrlL); action != ActionNone || len(out) != 0 || !p.Pending() {
					t.Fatalf("prefix: action=%v out=%v pending=%v", action, out, p.Pending())
				}
				if action, out := p.Feed(key); action != tc.want || len(out) != 0 || p.Pending() {
					t.Fatalf("key=%02x: action=%v out=%v pending=%v", key, action, out, p.Pending())
				}
				// These aliases consume one prefix; they never enter repeat mode.
				if action, out := p.Feed(tc.key); action != ActionNone || len(out) != 1 || out[0] != tc.key {
					t.Fatalf("unprefixed key: action=%v out=%v", action, out)
				}
			}
		})
	}
}
