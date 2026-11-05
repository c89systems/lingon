package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/sessionmanage"
)

type folderCall struct {
	ctx     context.Context
	request sessionmanage.Request
	reply   chan sessionmanage.Result
}

func folderTestPrompt(t *testing.T) (*DirectoryPrompt, chan folderCall, chan DirectoryView, *clock.MockClock) {
	t.Helper()
	calls := make(chan folderCall, 128)
	views := make(chan DirectoryView, 128)
	clk := clock.NewMock()
	caller := sessionmanage.CallerFunc(func(ctx context.Context, req sessionmanage.Request) (sessionmanage.Result, error) {
		call := folderCall{ctx: ctx, request: req, reply: make(chan sessionmanage.Result, 1)}
		calls <- call
		// Deliberately return even after cancellation to exercise stale reply handling.
		select {
		case result := <-call.reply:
			return result, nil
		case <-ctx.Done():
			return sessionmanage.Result{}, ctx.Err()
		}
	})
	p := &DirectoryPrompt{}
	p.Begin(t.Context(), DirectoryPromptOptions{SessionID: "remote", Caller: caller, Clock: clk, OnChange: func(view DirectoryView) { views <- view }})
	t.Cleanup(p.Close)
	return p, calls, views, clk
}
func folderView(t *testing.T, views chan DirectoryView, predicate func(DirectoryView) bool) DirectoryView {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case view := <-views:
			if predicate(view) {
				return view
			}
		case <-timeout.C:
			t.Fatal("folder view barrier")
			return DirectoryView{}
		}
	}
}
func folderRequest(t *testing.T, calls chan folderCall) folderCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("folder request barrier")
		return folderCall{}
	}
}

func TestDirectoryPromptCompletionAndCommit(t *testing.T) {
	p, calls, views, _ := folderTestPrompt(t)
	call := folderRequest(t, calls)
	call.reply <- sessionmanage.Result{Path: "/remote", Directories: []string{"/remote/alpha/", "/remote/beta/"}}
	initialView := folderView(t, views, func(v DirectoryView) bool { return v.Text == "/remote/" && v.Hint == "" })
	if initialView.Ghost != "" {
		t.Fatal("prefilled directory implicitly selected a child")
	}
	for _, b := range []byte("\x1b[Z") {
		p.Feed(b)
	}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/remote/beta/" })
	p.Feed('\t')
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/remote/alpha/" })
	p.Feed('\t')
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/remote/beta/" })
	for _, b := range []byte("\x1b[Z") {
		p.Feed(b)
	}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/remote/alpha/" })
	select {
	case extra := <-calls:
		t.Fatalf("Tab descended or made a new query: %+v", extra.request)
	default:
	}
	accepted := make(chan bool, 1)
	go func() { accepted <- p.Feed('\r') }()
	commit := folderRequest(t, calls)
	if commit.request.Kind != "chdir" || commit.request.Path != "/remote/alpha/" || commit.request.SessionID != "remote" {
		t.Fatalf("%+v", commit.request)
	}
	commit.reply <- sessionmanage.Result{Path: "/remote/alpha"}
	<-accepted
	folderView(t, views, func(v DirectoryView) bool { return !v.Active })
	if p.Feed('x') {
		t.Fatal("closed prompt swallowed app input")
	}
}

func TestDirectoryPromptInitialInputOrderingErrorsAndCancellation(t *testing.T) {
	p, calls, views, clk := folderTestPrompt(t)
	initial := folderRequest(t, calls)
	typed := make(chan struct{})
	go func() {
		for _, b := range []byte("\x15/new") {
			p.Feed(b)
		}
		close(typed)
	}()
	initial.reply <- sessionmanage.Result{Path: "/old"}
	<-typed
	var latest folderCall
	for i := 0; i < 2; i++ {
		call := folderRequest(t, calls)
		if call.request.Path == "/" {
			latest = call
		}
	}
	if latest.request.Path != "/" {
		t.Fatalf("queued text lost: %+v", latest.request)
	}
	latest.reply <- sessionmanage.Result{Path: "/old", Directories: []string{"/new child/"}}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/new" && v.Ghost == " child/" })
	accepted := make(chan bool, 1)
	go func() { accepted <- p.Feed('\r') }()
	commit := folderRequest(t, calls)
	commit.reply <- sessionmanage.Result{Error: "not a directory"}
	<-accepted
	folderView(t, views, func(v DirectoryView) bool { return v.Active && strings.Contains(v.Hint, "not a directory") })
	p.Feed(0x1b)
	clk.Add(searchEscapeDelay)
	folderView(t, views, func(v DirectoryView) bool { return !v.Active })
	if p.Feed('/') {
		t.Fatal("Escape leaked into next input")
	}
}

func TestDirectoryPromptExactNameAndDeadline(t *testing.T) {
	p, calls, views, clk := folderTestPrompt(t)
	initial := folderRequest(t, calls)
	initial.reply <- sessionmanage.Result{Path: "/root"}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/root/" && v.Hint == "" })
	for _, b := range []byte("\x15/a") {
		p.Feed(b)
	}
	var latest folderCall
	for i := 0; i < 2; i++ {
		call := folderRequest(t, calls)
		if call.request.Path == "/" {
			latest = call
		}
	}
	latest.reply <- sessionmanage.Result{Directories: []string{"/a-b/", "/a/"}}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/a" && v.Ghost == "/" })
	p.Feed('/')
	stalled := folderRequest(t, calls)
	clk.Add(15 * time.Second)
	select {
	case <-stalled.ctx.Done():
	default:
		t.Fatal("deadline did not cancel directory lookup")
	}
	folderView(t, views, func(v DirectoryView) bool { return strings.Contains(v.Hint, "timed out") })
	p.Feed(0x1b)
	clk.Add(searchEscapeDelay)
	folderView(t, views, func(v DirectoryView) bool { return !v.Active })
}

func TestDirectoryPromptPreservesPrefillWhenListingFails(t *testing.T) {
	p, calls, views, _ := folderTestPrompt(t)
	call := folderRequest(t, calls)
	call.reply <- sessionmanage.Result{Path: "/large-directory", Error: "too many matching directories"}
	view := folderView(t, views, func(v DirectoryView) bool { return strings.Contains(v.Hint, "too many") })
	if view.Text != "/large-directory/" || !view.Active {
		t.Fatalf("prefill lost: %+v", view)
	}
	p.Feed(0x03)
	folderView(t, views, func(v DirectoryView) bool { return !v.Active })
}

func TestDirectoryPromptDoesNotCommitSanitizedUnsafePath(t *testing.T) {
	p, calls, views, _ := folderTestPrompt(t)
	call := folderRequest(t, calls)
	call.reply <- sessionmanage.Result{Path: "/owner"}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/owner/" && v.Hint == "" })
	for _, b := range []byte("\x15\u202e") {
		p.Feed(b)
	}
	var latest folderCall
	for i := 0; i < 2; i++ {
		call := folderRequest(t, calls)
		if call.request.Path == "\u202e" {
			latest = call
		}
	}
	latest.reply <- sessionmanage.Result{Error: "invalid path"}
	folderView(t, views, func(v DirectoryView) bool { return v.Hint == "invalid path" })
	if !p.Feed('\r') {
		t.Fatal("Enter escaped the prompt")
	}
	folderView(t, views, func(v DirectoryView) bool { return v.Active && strings.Contains(v.Hint, "control characters") })
	select {
	case call := <-calls:
		t.Fatalf("unsafe name was sanitized into a different request: %+v", call.request)
	default:
	}
}

func TestDirectoryPromptReusesOneLevelAndCyclesOnlyMatchingDirs(t *testing.T) {
	p, calls, views, _ := folderTestPrompt(t)
	initial := folderRequest(t, calls)
	initial.reply <- sessionmanage.Result{Path: "/root", Directories: []string{"/root/alpha/", "/root/alpine/", "/root/beta/"}}
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/root/" && v.Hint == "" })
	p.Feed('a')
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/root/a" && v.Ghost == "lpha/" && v.Hint == "" })
	p.Feed('l')
	folderView(t, views, func(v DirectoryView) bool { return v.Text == "/root/al" && v.Ghost == "pha/" && v.Hint == "" })
	for _, want := range []string{"/root/alpha/", "/root/alpine/", "/root/alpha/"} {
		p.Feed('\t')
		folderView(t, views, func(v DirectoryView) bool { return v.Text == want && v.Hint == "" })
	}
	// Descending changes the parent directory and requires exactly a new level.
	p.Feed('d')
	query := folderRequest(t, calls)
	if query.request.Path != "/root/alpha/d" {
		t.Fatalf("not a one-level descent: %+v", query.request)
	}
	query.reply <- sessionmanage.Result{Directories: []string{"/root/alpha/deep/"}}
	folderView(t, views, func(v DirectoryView) bool { return v.Ghost == "eep/" })
}
