package lifecycle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type originRunner struct {
	next   runner
	remote string
}

func (r originRunner) run(ctx context.Context, c command) ([]byte, error) {
	if c.Name == "git" && strings.Join(c.Args, " ") == "remote get-url origin" {
		return []byte(r.remote), nil
	}
	return r.next.run(ctx, c)
}

func TestPublishPreconditionsForTransferredRepository(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:c89systems/lingon.git",
		"https://github.com/c89systems/lingon.git",
		"https://github.com/c89systems/lingon",
		"ssh://git@github.com/c89systems/lingon.git",
	} {
		t.Run(remote, func(t *testing.T) {
			w, f := testWorkflow(t)
			w.r = originRunner{next: f, remote: remote}
			if err := w.publishPreconditions(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 1 || f.calls[0].Name != "gh" || strings.Join(f.calls[0].Args, " ") != "api repos/c89systems/lingon/immutable-releases -H X-GitHub-Api-Version: 2026-03-10" {
				t.Fatalf("expected only immutability read on the transferred repository: %+v", f.calls)
			}
		})
	}
	w, f := testWorkflow(t)
	w.r = originRunner{next: f, remote: "git@github.com:unrelated-owner/lingon.git"}
	if err := w.publishPreconditions(context.Background()); err == nil {
		t.Fatal("unrelated origin accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("GitHub contacted for unrelated origin: %+v", f.calls)
	}
}

func TestGitHubPublicationTargetsTransferredRepository(t *testing.T) {
	w, f := testWorkflow(t)
	r := receipt{Version: "1.2.3"}
	f.version = r.Version
	for _, name := range expectedArtifacts(r.Version) {
		writeTestFile(t, filepath.Join(w.dist(r.Version), name), []byte(name))
	}
	if _, err := makeChecksums(w.dist(r.Version), r.Version); err != nil {
		t.Fatal(err)
	}
	if err := w.publishGitHub(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, c := range f.calls {
		if c.Name != "gh" {
			continue
		}
		if !strings.Contains(strings.Join(c.Args, " "), "--repo c89systems/lingon") {
			t.Fatalf("wrong GitHub target: %+v", c)
		}
		seen[c.Args[1]] = true
	}
	for _, operation := range []string{"list", "create", "view", "upload", "download", "edit"} {
		if !seen[operation] {
			t.Fatalf("publication did not exercise %s", operation)
		}
	}
}
