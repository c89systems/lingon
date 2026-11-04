package control

import "testing"

func TestPopupAndLifecycleKeysAreDistinct(t *testing.T) {
	keys := []byte{'j', 'J', 'q', 'Q', 'c', 'C', 'h'}
	seen := make(map[Action]byte)
	for _, key := range keys {
		var p Prefix
		p.Feed(CtrlL)
		action, out := p.Feed(key)
		if action == ActionNone || len(out) != 0 {
			t.Fatalf("Ctrl+L %c was not consumed as a command", key)
		}
		if previous, ok := seen[action]; ok {
			t.Fatalf("Ctrl+L %c and %c share an action", previous, key)
		}
		seen[action] = key
	}
}
