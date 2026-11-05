package sessionmanage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDirectoryChangeAndOneLevelCompletion(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"alpha", "alpine", "space dir", ".hidden", "alpha/deep"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "afile"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "alpha"), filepath.Join(base, "alink")); err != nil {
		t.Fatal(err)
	}
	directory := NewDirectory(base)
	ctx := context.Background()
	result, err := directory.Complete(ctx, "a")
	if err != nil || !reflect.DeepEqual(result.Directories, []string{"alink/", "alpha/", "alpine/"}) {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = directory.Complete(ctx, "alpha/")
	if err != nil || !reflect.DeepEqual(result.Directories, []string{"alpha/deep/"}) {
		t.Fatalf("%+v %v", result, err)
	}
	before, _ := os.Getwd()
	if path, err := directory.Change(ctx, "space dir"); err != nil || path != filepath.Join(base, "space dir") {
		t.Fatalf("%s %v", path, err)
	}
	after, _ := os.Getwd()
	if before != after {
		t.Fatal("changed process cwd")
	}
	for _, path := range []string{"missing", "../afile", "bad\npath"} {
		if _, err := directory.Change(ctx, path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if path, _ := directory.Path(); path != filepath.Join(base, "space dir") {
		t.Fatal("failed change mutated directory")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := directory.Change(cancelled, base); err == nil {
		t.Fatal("cancelled mutation succeeded")
	}
	if _, err := directory.Complete(cancelled, base+"/"); err == nil {
		t.Fatal("cancelled query succeeded")
	}
}

func TestDirectoryTildeAndValidation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := NewDirectory(t.TempDir())
	if path, err := dir.Change(context.Background(), "~"); err != nil || path != home {
		t.Fatalf("%q %v", path, err)
	}
	for _, req := range []Request{
		{Kind: "chdir"}, {Kind: "directory", Path: "bad\npath"}, {Kind: "create", Path: "/tmp"}, {Kind: "directory", Name: "bad"}, {Kind: "chdir", Path: "/tmp", Cols: 80, Rows: 24},
	} {
		req.SessionID = "s"
		req.RequestID = "r"
		if Validate(req) == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}

func TestDirectoryRootAndBoundedListings(t *testing.T) {
	dir := NewDirectory("/")
	result, err := dir.Complete(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range result.Directories {
		if strings.HasPrefix(path, "//") {
			t.Fatal("root completion doubled slash")
		}
	}
	base := t.TempDir()
	for i := 0; i < 4097; i++ {
		if err := os.Mkdir(filepath.Join(base, fmt.Sprintf("dir-%04d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	dir = NewDirectory(base)
	if result, err := dir.Complete(t.Context(), ""); err == nil || result.Path != base || len(result.Directories) != 0 {
		t.Fatal("oversized listing was silently truncated")
	}
	if result, err := dir.Complete(t.Context(), "dir-4096"); err != nil || len(result.Directories) != 1 {
		t.Fatalf("refined listing: %+v %v", result, err)
	}
}

func TestDirectoryRejectsInaccessibleDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory access restrictions")
	}
	base := t.TempDir()
	blocked := filepath.Join(base, "blocked")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	dir := NewDirectory(base)
	if _, err := dir.Change(t.Context(), blocked); err == nil {
		t.Fatal("accepted inaccessible directory")
	}
	if path, _ := dir.Path(); path != base {
		t.Fatal("failed change mutated launch directory")
	}
	result, err := dir.Complete(t.Context(), blocked+"/")
	if err == nil || result.Path != base {
		t.Fatalf("completion lost current cwd: %+v %v", result, err)
	}
}
