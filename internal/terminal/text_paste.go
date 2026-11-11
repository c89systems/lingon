package terminal

import "strings"

// TextPaste encodes text for the application's current paste mode. Ordinary
// terminal input uses carriage returns; bracketed paste preserves newlines.
func TextPaste(text string, mode uint32) []byte {
	if text == "" {
		return nil
	}
	if mode&SnapshotModeBracketedPaste != 0 {
		return []byte("\x1b[200~" + text + "\x1b[201~")
	}
	return []byte(strings.ReplaceAll(text, "\n", "\r"))
}
