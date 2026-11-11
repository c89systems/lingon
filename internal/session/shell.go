package session

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/terminal"
)

func resolveShellPath(shellOverride string) string {
	path := shellOverride
	if path == "" {
		if u, err := user.Current(); err == nil && u != nil && u.Uid != "" {
			if shell, err := shellFromPasswd(u.Uid); err == nil && shell != "" {
				path = shell
			}
		}
	}
	if path == "" {
		path = os.Getenv("SHELL")
	}
	if path == "" {
		path = "/bin/sh"
	}
	return path
}

type shellStart struct {
	shell, term, directory string
	size                   terminal.WindowSize
	clipboard              *clipboard.Endpoint
}

func startShell(in shellStart) (*os.File, *os.File, *exec.Cmd, error) {
	path := resolveShellPath(in.shell)
	if strings.ContainsRune(path, filepath.Separator) && !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, nil, nil, err
		}
		path = absolute
	}
	cmd := exec.Command(path)
	cmd.Dir = in.directory
	// These variables describe the enclosing terminal, not this new PTY. Leaving
	// them exported makes programs override TIOCGWINSZ with stale parent sizes.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "LINES=") && !strings.HasPrefix(entry, "COLUMNS=") && (in.directory == "" || !strings.HasPrefix(entry, "PWD=")) {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if in.directory != "" {
		cmd.Env = append(cmd.Env, "PWD="+in.directory)
	}
	if in.term != "" {
		cmd.Env = append(cmd.Env, "TERM="+in.term)
	}
	if in.clipboard != nil {
		cmd.Env = in.clipboard.Env(cmd.Env)
	}
	ptyFile, ttyFile, err := pty.StartWithTTY(cmd, in.size)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("start shell: %w", err)
	}
	return ptyFile, ttyFile, cmd, nil
}
