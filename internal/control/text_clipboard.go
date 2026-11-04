package control

import "sync"

// TextClipboard keeps copied scrollback text inside one interactive process.
// Its owner shares it with tabs; it never reads or writes an OS clipboard.
type TextClipboard struct {
	mu   sync.RWMutex
	text string
}

// Store replaces the last copied text.
func (c *TextClipboard) Store(text string) { c.mu.Lock(); c.text = text; c.mu.Unlock() }

// Text returns the last copied text, or an empty string before the first copy.
func (c *TextClipboard) Text() string { c.mu.RLock(); defer c.mu.RUnlock(); return c.text }
