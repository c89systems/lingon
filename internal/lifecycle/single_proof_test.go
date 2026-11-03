package lifecycle

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseRunsExpensiveProofOnlyOnceOnFinalTag(t *testing.T) {
	w, f := testWorkflow(t)
	f.mutate = func(c command) {
		// Every compiler, test suite and artifact builder must see the final
		// release identity. Git/format/hygiene/tool-version preflight is cheap.
		expensive := c.Name == "go" || c.Name == "./gradlew" || c.Name == "golint" || c.Name == "podman" ||
			(c.Name == "bash" && len(c.Args) > 0 && c.Args[0] != "scripts/check-hygiene.sh")
		if !expensive {
			return
		}
		if branch := testGit(t, w, "symbolic-ref", "--short", "HEAD"); branch != "trunk" {
			t.Fatalf("expensive candidate work before final release: %s %v on %s", c.Name, c.Args, branch)
		}
		if tags := testGit(t, w, "tag", "--points-at", "HEAD"); tags != "v1.2.3" {
			t.Fatalf("work did not use final tagged identity: %s %v, tags=%q", c.Name, c.Args, tags)
		}
	}
	if err := w.release(t.Context(), Options{Version: "1.2.3", Message: "perf(fixture): prove a single final release"}); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, c := range f.calls {
		args := strings.Join(c.Args, " ")
		if c.Name == "go" && len(c.Args) > 0 && (c.Args[0] == "test" || c.Args[0] == "vet" || c.Args[0] == "mod") {
			counts[filepath.Base(c.Dir)+":"+args]++
		}
		if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "build" {
			counts["CLI archive builds"]++
		}
		if c.Name == "bash" && args == "android/scripts/run-integration-tests.sh" {
			switch {
			case c.Env["LINGON_IT_CLEARTEXT"] == "1":
				counts["Android cleartext"]++
			case c.Env["LINGON_IT_CLASS"] != "":
				counts["Android remaining classes"]++
			default:
				counts["Android full E2E"]++
			}
		}
		if c.Name == "golint" || (c.Name == "bash" && (strings.HasPrefix(args, "scripts/golangci-lint.sh ") || args == "scripts/check-install.sh")) {
			counts[c.Name+":"+args]++
		}
		if c.Name == "./gradlew" {
			for _, task := range []string{":app:testDebugUnitTest", ":app:compileDebugAndroidTestKotlin", ":app:assembleRelease"} {
				if strings.Contains(args, task) {
					counts[task]++
				}
			}
		}
		if c.Name == "podman" && strings.HasPrefix(args, "build ") {
			counts["container architecture builds"]++
		}
	}
	for key, want := range map[string]int{
		filepath.Base(w.root) + ":mod verify":                                                           1,
		filepath.Base(w.root) + ":vet ./...":                                                            1,
		filepath.Base(w.root) + ":test -count=1 -timeout=20m ./...":                                     1,
		filepath.Base(w.root) + ":test -race -count=1 -timeout=20m ./...":                               1,
		filepath.Base(w.root) + ":test -race -tags=integration -count=1 -timeout=30m ./integration/...": 1,
		"android:vet ./...": 1,
		"android:test -race -count=1 -timeout=10m ./...": 1,
		filepath.Base(w.root) + ":test ./internal/relay ./internal/terminal/emu -run=^$ -bench=Benchmark(WSReplay|AbsoluteCursorPositionScrollRegion)$ -benchmem -benchtime=1s -count=3": 1,
		"golint:-set_exit_status ./...":                           1,
		"bash:scripts/golangci-lint.sh run --concurrency=1 ./...": 1,
		"bash:scripts/check-install.sh":                           1,
		"CLI archive builds":                                      6, "container architecture builds": 2,
		"Android full E2E": 1, "Android cleartext": 1, "Android remaining classes": 1,
		":app:testDebugUnitTest": 1, ":app:compileDebugAndroidTestKotlin": 1, ":app:assembleRelease": 1,
	} {
		if got := counts[key]; got != want {
			t.Errorf("%s ran %d times, want %d", key, got, want)
		}
	}
	r, err := w.verified(t.Context())
	if err != nil || !r.FullProof || r.Version != "1.2.3" {
		t.Fatalf("single pass did not produce fully verified final artifacts: %+v, %v", r, err)
	}
}

func TestPreflightRejectsChangedReleaseInputsBeforeSquash(t *testing.T) {
	for _, input := range []string{"worktree", "branch", "local trunk", "remote trunk", "remote tag"} {
		t.Run(input, func(t *testing.T) {
			w, f := testWorkflow(t)
			candidate := testGit(t, w, "rev-parse", "HEAD")
			f.mutate = func(c command) {
				if c.Name != "less" {
					return
				}
				switch input {
				case "worktree":
					writeTestFile(t, filepath.Join(w.root, "feature.txt"), []byte("changed during preflight"))
				case "branch":
					testGit(t, w, "switch", "-c", "changed-branch")
				case "local trunk":
					testGit(t, w, "update-ref", "refs/heads/trunk", candidate)
				case "remote trunk":
					testGit(t, w, "push", "origin", candidate+":refs/heads/trunk")
				case "remote tag":
					testGit(t, w, "push", "origin", candidate+":refs/tags/v1.2.3")
				}
			}
			if err := w.release(t.Context(), Options{Version: "1.2.3", Message: "perf(fixture): retain pinned release inputs"}); err == nil {
				t.Fatal("release accepted changed input")
			}
			if tags := testGit(t, w, "tag", "--list"); tags != "" {
				t.Fatalf("release created tag after stale preflight: %s", tags)
			}
			for _, c := range f.calls {
				args := strings.Join(c.Args, " ")
				if c.Name == "./gradlew" || c.Name == "go" || c.Name == "podman" || c.Name == "gh" || (c.Name == "git" && (strings.HasPrefix(args, "merge --squash") || strings.HasPrefix(args, "commit "))) {
					t.Fatalf("stale preflight reached release work: %s %s", c.Name, args)
				}
			}
		})
	}
}
