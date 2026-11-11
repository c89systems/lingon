//go:build integration

package integrationptyattach_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/terminal"
)

func TestNeovimSpellingUnderlinePTY(t *testing.T) {
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Skip("Neovim not installed; synthetic underline PTY regression is unconditional")
	}
	dir := t.TempDir()
	init := filepath.Join(dir, "init.vim")
	// Explicit highlights avoid personal settings/themes. SpellBad is applied by
	// Neovim's real spell checker. The known-good word has an ordinary underline.
	script := `set termguicolors
set noswapfile noundofile
set spell spelllang=en
call setline(1, ['', 'cai ordinary', 'NVIM-UNDERLINE-READY'])
highlight Normal guifg=#e0e0e0 guibg=#262230
highlight SpellBad gui=undercurl guisp=#d7875f
highlight UnderlineProbe gui=underline guifg=#e0e0e0 guibg=#262230
call matchadd('UnderlineProbe', 'ordinary')
normal! G
`
	if err := os.WriteFile(init, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(dir, "nvim-shell")
	// The host starts this entire program tree in its dedicated child PTY. No
	// inherited tty, user configuration, swap files, or editor server is used.
	body := "#!/bin/sh\nunset NVIM NVIM_LISTEN_ADDRESS\nexport TERM=xterm-256color COLORTERM=truecolor\nexec '" + nvim + "' --clean -u NONE -i NONE -n -S '" + init + "'\n"
	if err := os.WriteFile(shell, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"direct", "host", "attach"} {
		t.Run(mode, func(t *testing.T) {
			rec := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clock.NewMock()), ptytest.WithWSRecorder(rec))
			var host *ptytest.PTYSession
			if mode == "direct" {
				host = startDirectUnderlineEditor(t, shell)
			} else {
				host = h.StartHost(ptytest.HostOptions{SessionID: "nvim-spell", Shell: shell, Cols: 80, Rows: 20})
			}
			t.Cleanup(host.Cancel)
			popupScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("NVIM-UNDERLINE-READY") && s.Contains("cai ordinary") })
			viewer := host
			if mode == "attach" {
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				if err := rec.WaitForFrame(ctx, func(frame ptytest.FrameRecord) bool {
					return frame.Role == "host" && frame.Direction == ptytest.DirClientToServer && frame.SessionID == "nvim-spell" && frame.Payload == "snapshot"
				}); err != nil {
					t.Fatal(err)
				}
				viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "nvim-spell", Cols: 80, Rows: 20})
				t.Cleanup(viewer.Cancel)
				popupScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("NVIM-UNDERLINE-READY") && s.Contains("cai ordinary") })
			}
			// A completed command-line message (not its input echo) fences the editor frame.
			viewer.Send(":redraw! | echo 'NVIM-DRAW-DONE'\r")
			popupScreen(t, viewer, func(s ptytest.Screen) bool { return strings.TrimSpace(s.Row(s.Rows-1)) == "NVIM-DRAW-DONE" })
			snap := viewer.Snapshot()
			for _, x := range []int{0, 1, 2, 4, 5, 6, 7, 8, 9, 10, 11} {
				cell := snap.Cells[80+x]
				wantMode, wantColor := terminal.ModeUnderline, uint32(0)
				if x < 3 {
					wantMode, wantColor = terminal.ModeUnderlineCurly, terminal.ColorTrue|0xd7875f
				}
				if cell.Mode != wantMode || cell.UnderlineColor != wantColor || cell.FG != terminal.ColorTrue|0xe0e0e0 || cell.BG != terminal.ColorTrue|0x262230 {
					t.Fatalf("Neovim cell %d differs from requested highlight: %+v", x, cell)
				}
			}
			raw := viewer.DrainRaw()
			if !strings.Contains(raw, "4:3") {
				t.Fatalf("Neovim SpellBad lost its curly underline: %q", raw)
			}
			if !strings.Contains(raw, "58;2;215;135;95") && !strings.Contains(raw, "58:2::215:135:95") {
				t.Fatalf("Neovim SpellBad lost its underline color: %q", raw)
			}
		})
	}
}

// The standalone editor baseline owns its own session, controlling TTY and all
// stdio. Cleanup signals only that newly created process group and joins it.
func startDirectUnderlineEditor(t *testing.T, shell string) *ptytest.PTYSession {
	t.Helper()
	master, slave := ptytest.OpenPTY(t, 80, 20)
	viewer := ptytest.NewPTYSessionWithClock(t, master, slave, 80, 20, clock.NewMock())
	cmd := exec.Command(shell)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		viewer.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		viewer.Close()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Error("isolated Neovim did not exit")
		}
	})
	return viewer
}
