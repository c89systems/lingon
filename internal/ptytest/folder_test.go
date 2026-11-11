package ptytest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

type folderRespawnClock struct {
	*clock.MockClock
	waiting chan struct{}
}

func (c *folderRespawnClock) Sleep(delay time.Duration) {
	if delay != 100*time.Millisecond {
		c.MockClock.Sleep(delay)
		return
	}
	timer := c.NewTimer(delay)
	defer timer.Stop()
	c.waiting <- struct{}{}
	<-timer.C
}

func folderShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shell")
	script := "#!/bin/sh\nprintf '\\nREADY> '\nwhile IFS= read -r line; do case \"$line\" in where*) printf '\\n%s:%s\\n' \"$line\" \"$(pwd)\";; leave) exit 0;; *) printf '\\nREPLY:%s\\n' \"$line\";; esac; printf 'READY> '; done\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func folderField(t *testing.T, viewer *ptytest.PTYSession, want string) {
	t.Helper()
	waitSearchScreen(t, viewer, func(s ptytest.Screen) bool {
		return strings.HasPrefix(strings.TrimSpace(s.Row(s.Rows-1)), "working directory: "+want) && !strings.Contains(s.Row(s.Rows-1), "loading")
	})
}

func TestFolderHostAndAttachPTY(t *testing.T) {
	for _, mode := range []string{"host", "multi", "single"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			destination := filepath.Join(base, "folder with spaces")
			for _, name := range []string{"folder with spaces", "folder with spaces/alpha", "folder with spaces/beta", "other"} {
				if err := os.MkdirAll(filepath.Join(base, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(destination, "afile"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			clk := &folderRespawnClock{MockClock: clock.NewMock(), waiting: make(chan struct{}, 4)}
			recorder := ptytest.NewWSRecorder()
			h := ptytest.New(t, ptytest.WithClock(clk), ptytest.WithWSRecorder(recorder))
			host := h.StartHost(ptytest.HostOptions{Respawn: true, SessionID: "folder", SessionName: "folder", WorkingDirectory: base, Shell: folderShell(t), Cols: 220, Rows: 20})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			viewer := host
			if mode != "host" {
				waitRestoreHost(t, recorder, "folder")
				if mode == "multi" {
					viewer = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: "folder", Cols: 220, Rows: 20})
				} else {
					viewer = h.StartAttach(ptytest.AttachOptions{SessionID: "folder", RequestControl: true, Cols: 220, Rows: 20})
				}
				t.Cleanup(viewer.Cancel)
				waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			}
			viewer.Send("\x0cf")
			folderField(t, viewer, base+"/")
			// Prefill is the owner's configured launch directory, and is editable in-place.
			viewer.Send("folder\t")
			folderField(t, viewer, destination+"/")
			viewer.Send("\rwhere-old\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("where-old:" + base) })
			viewer.Send("\x0cf")
			folderField(t, viewer, destination+"/")
			viewer.Send("a")
			folderField(t, viewer, destination+"/alpha/") // Ghost text excludes afile.
			viewer.Send("\t")
			folderField(t, viewer, destination+"/alpha/")
			// Cancel leaves the configured directory unchanged, even after completion.
			viewer.Send("\x03\x0cf")
			folderField(t, viewer, destination+"/")
			viewer.Send("\x15" + filepath.Join(base, "absent") + "\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return strings.Contains(s.Row(19), "no such file") })
			viewer.Send("\x03\x0cf")
			folderField(t, viewer, destination+"/")
			viewer.Send("\x03cancel-fence\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("REPLY:cancel-fence") })
			// A respawn, unlike the existing application, starts in the updated directory.
			viewer.Send("leave\r")
			// The test-owned clock reports registration of the respawn timer,
			// so advancing mock time never races the old shell's exit.
			select {
			case <-clk.waiting:
			case <-time.After(8 * time.Second):
				t.Fatal("respawn timer barrier")
			}
			clk.Add(100 * time.Millisecond)
			viewer.Send("where-respawn\r")
			waitSearchScreen(t, viewer, func(s ptytest.Screen) bool { return s.Contains("where-respawn:" + destination) })
		})
	}
}

func TestFolderNewLocalTabAndPopupInheritPTY(t *testing.T) {
	for _, popup := range []bool{false, true} {
		t.Run(fmt.Sprint(popup), func(t *testing.T) {
			base := t.TempDir()
			destination := filepath.Join(base, "child")
			if err := os.Mkdir(destination, 0700); err != nil {
				t.Fatal(err)
			}
			h := ptytest.New(t)
			host := h.StartHost(ptytest.HostOptions{SessionID: "folder-parent", WorkingDirectory: base, Shell: folderShell(t), Cols: 200, Rows: 24})
			t.Cleanup(host.Cancel)
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("READY>") })
			host.Send("\x0cf")
			folderField(t, host, base+"/")
			host.Send("child\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return !strings.Contains(s.Row(23), "working directory:") })
			key := "c"
			if popup {
				key = "j"
			}
			host.Send("\x0c" + key + "where-child\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("where-child:" + destination) })
			if popup {
				host.Send("\x0cj")
			} else {
				host.Send("\x0cp")
			}
			host.Send("where-parent\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("where-parent:" + base) })
			// A later parent change does not retarget an existing tab or popup.
			host.Send("\x0cf")
			folderField(t, host, destination+"/")
			host.Send("\x15" + base + "\r\x0c" + map[bool]string{true: "j", false: "n"}[popup] + "\x0cf")
			folderField(t, host, destination+"/")
			host.Send("\x03existing-child\r")
			waitSearchScreen(t, host, func(s ptytest.Screen) bool { return s.Contains("REPLY:existing-child") })
		})
	}
}
