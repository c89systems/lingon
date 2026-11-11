package sessionmanage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// Caller executes management on the machine that owns a session.
type Caller interface {
	Manage(context.Context, Request) (Result, error)
}

// CallerFunc adapts host-local routing to Caller.
type CallerFunc func(context.Context, Request) (Result, error)

// Manage executes the adapted call.
func (f CallerFunc) Manage(ctx context.Context, req Request) (Result, error) { return f(ctx, req) }

// ValidatePath rejects unbounded paths and characters unsafe to show in a terminal.
func ValidatePath(path string) error {
	if len(path) > 4096 || !utf8.ValidString(path) {
		return fmt.Errorf("working directory must be valid UTF-8 and at most 4096 bytes")
	}
	for _, r := range path {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("working directory must not contain control characters")
		}
	}
	return nil
}

// Directory holds one session's launch directory, independently of process cwd.
// Child sessions copy the value; a respawn reads its session's latest value.
type Directory struct {
	mu   sync.Mutex
	path string
}

// NewDirectory initializes a session launch directory. Empty uses process cwd
// on first access, before the session's first application is started.
func NewDirectory(path string) *Directory { return &Directory{path: path} }

func (d *Directory) currentLocked() (string, error) {
	if d.path == "" {
		path, err := os.Getwd()
		if err != nil {
			return "", err
		}
		d.path = path
	}
	absolute, err := filepath.Abs(d.path)
	if err != nil {
		return "", err
	}
	d.path = absolute
	return d.path, nil
}

// Path returns the absolute launch directory without changing any process cwd.
func (d *Directory) Path() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentLocked()
}

func directoryPath(base, path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path), nil
}

// Change validates the destination and atomically updates future launches only.
func (d *Directory) Change(ctx context.Context, path string) (string, error) {
	if err := ValidatePath(path); err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	base, err := d.currentLocked()
	if err != nil {
		return "", err
	}
	path, err = directoryPath(base, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", path)
	}
	if err := unix.Access(path, unix.X_OK); err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	d.path = path
	return path, nil
}

// Complete scans only the immediate directory containing the typed prefix.
// Symlinks to directories qualify; files and unsafe names do not. Large listings
// fail explicitly so the caller can refine the prefix, rather than truncate.
func (d *Directory) Complete(ctx context.Context, prefix string) (Result, error) {
	if err := ValidatePath(prefix); err != nil {
		return Result{}, err
	}
	base, err := d.Path()
	if err != nil {
		return Result{}, err
	}
	if prefix == "" {
		prefix = strings.TrimRight(base, string(filepath.Separator)) + string(filepath.Separator)
	}
	parent, leaf := filepath.Split(prefix)
	path, err := directoryPath(base, parent)
	if err != nil {
		return Result{Path: base}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{Path: base}, err
	}
	// Reject non-directories at open time: opening an unpaired FIFO for reading
	// would block before cancellation or ReadDir can be checked.
	dir, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK, 0)
	if err != nil {
		return Result{Path: base}, fmt.Errorf("directory completion: %w", err)
	}
	defer dir.Close()
	result := Result{Path: base}
	size := 0
	for {
		if err := ctx.Err(); err != nil {
			return Result{Path: base}, err
		}
		entries, readErr := dir.ReadDir(128)
		if readErr != nil && readErr != io.EOF {
			return Result{Path: base}, readErr
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), leaf) || ValidatePath(entry.Name()) != nil {
				continue
			}
			isDir := entry.IsDir()
			if entry.Type()&os.ModeSymlink != 0 {
				if info, err := os.Stat(filepath.Join(path, entry.Name())); err == nil {
					isDir = info.IsDir()
				}
			}
			if !isDir {
				continue
			}
			candidate := parent + entry.Name() + string(filepath.Separator)
			size += len(candidate)
			if len(result.Directories) >= 4096 || size > 256*1024 {
				return Result{Path: base}, fmt.Errorf("too many matching directories; type a more specific prefix")
			}
			result.Directories = append(result.Directories, candidate)
		}
		if readErr == io.EOF {
			break
		}
	}
	sort.Strings(result.Directories)
	return result, nil
}
