package session

import (
	"context"
	"os"
	"time"

	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/terminal"
)

func (r *Runner) syncCopySourceLocked(frame localScrollbackSnapshot, rows int) {
	r.scrollbackView.SyncSource(mvu.ScrollbackSource{Position: mvu.ScrollbackPosition{End: frame.end}, HistoryRows: len(frame.rows), LiveRows: int(frame.live.GetRows()), ViewRows: rows})
}

func (r *Runner) scrollbackCopy(selectLines bool, stdout, stdin *os.File) {
	id, localFocus := r.focusedSession()
	if !localFocus {
		if r.remoteSessions != nil {
			r.remoteSessions.ScrollbackCopy(id, selectLines)
		}
		return
	}
	local := r.localSession(id)
	if local == nil {
		return
	}
	cols, rows := r.focusedViewportSize(stdout, stdin)
	r.scrollbackMu.Lock()
	if !r.scrollbackView.Active() || r.scrollbackSession != id {
		r.scrollbackMu.Unlock()
		return
	}
	frame := local.scrollbackFrameState()
	if frame.live == nil {
		r.scrollbackMu.Unlock()
		return
	}
	r.syncCopySourceLocked(frame, rows)
	r.scrollbackView.Normalize(len(frame.rows)+int(frame.live.Rows), rows, scrollbackContentWidthTerminal(frame.rows, frame.live), cols)
	if !selectLines {
		r.scrollbackView.ToggleVisual()
	} else if selected, copy := r.scrollbackView.SelectLines(); copy {
		r.textClipboard.Store(mvu.CopyScrollbackLines(mvu.NewTerminalTextSource(frame.rows, frame.live), selected))
	}
	r.scrollbackMu.Unlock()
	r.renderScrollback(stdout, stdin)
}

func (r *Runner) pasteCopiedText(ctx context.Context) {
	text := r.textClipboard.Text()
	if text == "" {
		return
	}
	id, localFocus := r.focusedSession()
	var err error
	if localFocus {
		if local := r.localSession(id); local != nil {
			_, err = local.writePTY(terminal.TextPaste(text, local.Snapshot().GetMode()))
		}
	} else if r.remoteSessions != nil {
		err = r.remoteSessions.PasteText(ctx, id, text, r.stdout())
	}
	if err != nil {
		r.showErrorStatus("Paste: "+err.Error(), r.stdout(), 3*time.Second)
	}
}
