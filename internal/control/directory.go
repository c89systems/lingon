package control

import (
	"context"
	"crypto/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/sessionmanage"
)

// DirectoryLabel identifies the launch-directory editor.
const DirectoryLabel = "working directory: "

// DirectoryView is the folder prompt's printable, atomic presentation.
type DirectoryView struct {
	Active            bool
	Text, Ghost, Hint string
}

// DirectoryPromptOptions binds the editor to one immutable session on its owner.
type DirectoryPromptOptions struct {
	SessionID string
	Caller    sessionmanage.Caller
	Clock     clock.Clock
	OnChange  func(DirectoryView)
}

// DirectoryPrompt queries directories asynchronously and reuses search's editor.
// Tab and Enter await their outstanding request before processing later keys;
// relay reads and screen updates are independent of this input worker.
type DirectoryPrompt struct {
	mu, renderMu                 sync.Mutex
	search                       SearchPrompt
	ctx                          context.Context
	cancel, requestCancel        context.CancelFunc
	requestCtx                   context.Context
	ready                        <-chan struct{}
	options                      DirectoryPromptOptions
	generation                   uint64
	initialized, loading, saving bool
	cacheReady                   bool
	prefix, level, match         string
	candidates                   []SearchSession
	hint                         string
}

// Begin loads the owner's current directory and its immediate subdirectories.
func (p *DirectoryPrompt) Begin(ctx context.Context, options DirectoryPromptOptions) {
	p.Close()
	if options.Clock == nil {
		options.Clock = clock.New()
	}
	p.mu.Lock()
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.options = options
	p.initialized, p.loading, p.saving = false, false, false
	p.candidates = nil
	p.cacheReady = false
	p.prefix, p.level, p.match = "", "", ""
	p.hint = ""
	p.search.Begin(options.Clock, p.notify, "")
	p.search.mu.Lock()
	p.search.editor.limit = 4096
	p.search.mu.Unlock()
	p.requestLocked("", true, false)
	p.mu.Unlock()
	p.notify()
}

// Close cancels outstanding work when the input owner exits or replaces a prompt.
func (p *DirectoryPrompt) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	if p.requestCancel != nil {
		p.requestCancel()
	}
	p.generation++
	p.search.Close()
}

// HandlesInput includes cancellation sequence tails, keeping them out of the PTY.
func (p *DirectoryPrompt) HandlesInput() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.search.HandlesInput()
}

func directoryParent(path string) string { return path[:strings.LastIndexByte(path, '/')+1] }

func (p *DirectoryPrompt) sourcesLocked() []SearchSession {
	sources := make([]SearchSession, 0, len(p.candidates))
	for _, candidate := range p.candidates {
		if strings.HasPrefix(candidate.Name, p.match) {
			sources = append(sources, candidate)
		}
	}
	return sources
}

func (p *DirectoryPrompt) viewLocked() DirectoryView {
	// Prefer a directory whose name is already fully typed over longer siblings.
	p.search.mu.Lock()
	if !p.search.completed {
		text := string(p.search.editor.text)
		for _, candidate := range p.candidates {
			if candidate.Name == text+"/" {
				p.search.selected = candidate.ID
				break
			}
		}
	}
	p.search.mu.Unlock()
	view := p.search.View(p.sourcesLocked())
	ghost := view.Ghost
	// An untouched full directory must not implicitly select one of its children.
	p.search.mu.Lock()
	if strings.HasSuffix(string(p.search.editor.text), "/") && !p.search.completed {
		ghost = ""
	}
	p.search.mu.Unlock()
	return DirectoryView{Active: view.Active, Text: view.Text, Ghost: ghost, Hint: printableName(p.hint)}
}

func (p *DirectoryPrompt) notify() {
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	p.mu.Lock()
	view := p.viewLocked()
	callback, cancel := p.options.OnChange, p.cancel
	p.mu.Unlock()
	if callback != nil {
		callback(view)
	}
	if !view.Active && cancel != nil {
		cancel()
	}
}

func (p *DirectoryPrompt) requestLocked(path string, initial, saving bool) {
	if p.requestCancel != nil {
		p.requestCancel()
	}
	ctx, cancel := context.WithCancelCause(p.ctx)
	p.requestCancel = func() { cancel(context.Canceled) }
	p.requestCtx = ctx
	done := make(chan struct{})
	p.ready = done
	p.generation++
	generation := p.generation
	root, caller, id := p.ctx, p.options.Caller, p.options.SessionID
	p.saving, p.loading = saving, !saving
	p.cacheReady = false
	p.prefix, p.level = path, directoryParent(path)
	p.hint = "loading…"
	if saving {
		p.hint = "saving…"
	} else {
		p.candidates = nil
	}
	timer := p.options.Clock.AfterFunc(15*time.Second, func() {
		cancel(context.DeadlineExceeded)
		p.mu.Lock()
		valid := generation == p.generation && root.Err() == nil && p.search.View(nil).Active
		if valid {
			p.loading, p.saving, p.initialized = false, false, true
			p.hint = "directory request timed out; reload to confirm its outcome"
		}
		p.mu.Unlock()
		if valid {
			p.notify()
		}
	})
	go func() {
		defer close(done)
		defer timer.Stop()
		kind := "directory"
		if saving {
			kind = "chdir"
		}
		result, err := caller.Manage(ctx, sessionmanage.Request{RequestID: rand.Text(), SessionID: id, Kind: kind, Path: path})
		p.mu.Lock()
		if generation != p.generation || root.Err() != nil || !p.search.View(nil).Active {
			p.mu.Unlock()
			return
		}
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		p.loading, p.saving, p.initialized = false, false, true
		p.hint = ""
		if initial && result.Path != "" {
			p.search.mu.Lock()
			p.search.editor.text = []byte(strings.TrimRight(result.Path, "/") + "/")
			p.match = string(p.search.editor.text)
			p.prefix, p.level = p.match, directoryParent(p.match)
			p.search.mu.Unlock()
		}
		if err != nil {
			p.hint = err.Error()
			if context.Cause(ctx) == context.DeadlineExceeded {
				p.hint = "directory request timed out; reload to confirm its outcome"
			}
		} else if result.Error != "" {
			p.hint = result.Error
		} else if saving {
			p.search.mu.Lock()
			p.search.editor.Active = false
			p.search.mu.Unlock()
		} else {
			p.cacheReady = true
			p.candidates = nil
			for _, directory := range result.Directories {
				p.candidates = append(p.candidates, SearchSession{ID: directory, Name: directory})
			}
		}
		p.mu.Unlock()
		p.notify()
	}()
}

// Feed edits locally. Escape cancels; Ctrl+U clears; Ctrl+W removes a path
// component. Tab cycles one level, Shift+Tab reverses, and Enter saves on the owner.
func (p *DirectoryPrompt) Feed(b byte) bool {
	p.mu.Lock()
	if !p.search.HandlesInput() {
		p.mu.Unlock()
		return false
	}
	p.search.mu.Lock()
	active := p.search.editor.Active
	escape := p.search.escapePrefix
	p.search.mu.Unlock()
	if !active {
		result := p.search.Feed(b, nil)
		p.mu.Unlock()
		return result.Consumed
	}
	reverse := escape == 2 && b == 'Z'
	if reverse {
		p.search.mu.Lock()
		p.search.escapeTimer.Stop()
		if !p.search.completed && strings.HasSuffix(string(p.search.editor.text), "/") {
			p.search.completed = true
		}
		p.search.mu.Unlock()
	}
	if b != 0x1b && b != 0x03 && ((escape == 0 && !p.initialized) || (p.loading && (b == '\t' || b == '\r' || b == '\n' || reverse))) {
		ready, ctx := p.ready, p.requestCtx
		p.mu.Unlock()
		select {
		case <-ready:
			return p.Feed(b)
		case <-ctx.Done():
			return true
		}
	}
	if escape == 0 && (b == '\r' || b == '\n') {
		view := p.viewLocked()
		p.search.mu.Lock()
		raw := string(p.search.editor.text)
		p.search.mu.Unlock()
		if err := sessionmanage.ValidatePath(raw + view.Ghost); err != nil {
			p.hint = err.Error()
			p.mu.Unlock()
			p.notify()
			return true
		}
		p.requestLocked(raw+view.Ghost, false, true)
		ready, ctx := p.ready, p.requestCtx
		p.mu.Unlock()
		p.notify()
		select {
		case <-ready:
		case <-ctx.Done():
		}
		return true
	}
	if escape == 0 && (b == 0x15 || b == 0x17) {
		p.search.mu.Lock()
		text := strings.TrimRight(string(p.search.editor.text), "/")
		if b == 0x15 {
			text = ""
		} else {
			text = text[:strings.LastIndexByte(text, '/')+1]
		}
		p.search.editor.text = []byte(text)
		p.search.selected = ""
		p.search.completed = false
		p.search.mu.Unlock()
	} else {
		result := p.search.Feed(b, p.sourcesLocked())
		if !result.Consumed {
			p.mu.Unlock()
			p.notify()
			return false
		}
	}
	if escape == 0 && b != 0x1b && b != 0x03 && b != '\t' {
		p.search.mu.Lock()
		text := string(p.search.editor.text)
		p.search.mu.Unlock()
		if utf8.ValidString(text) && p.search.View(nil).Active {
			p.match = text
			// A result for a broader prefix is also sufficient for a narrower
			// prefix at that same level, including while the read is in flight.
			reusable := (p.loading || p.cacheReady) && directoryParent(text) == p.level && strings.HasPrefix(text, p.prefix) && (p.prefix != "" || text == "")
			if !reusable {
				p.requestLocked(text, false, false)
			} else if !p.loading {
				p.hint = ""
			}
		}
	}
	p.mu.Unlock()
	p.notify()
	return true
}
