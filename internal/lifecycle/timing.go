package lifecycle

import (
	"context"
	"fmt"
	"io"
	"time"

	"pkt.systems/lingon/internal/clock"
)

// timedRunner records command timings outside the returned command output, so
// native instrumentation receipts and bounded output tails retain their shape.
type timedRunner struct {
	next          runner
	clock         clock.Clock
	log, progress io.Writer
}

func (r timedRunner) run(ctx context.Context, c command) ([]byte, error) {
	started := r.clock.Now()
	fmt.Fprintf(r.log, "\nCommand started: %s\n", started.UTC().Format(time.RFC3339Nano))
	out, err := r.next.run(ctx, c)
	finished := r.clock.Now()
	elapsed := finished.Sub(started)
	status := "passed"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(r.log, "\nCommand finished: %s elapsed=%s status=%s\n", finished.UTC().Format(time.RFC3339Nano), elapsed, status)
	if c.Label != "" && r.progress != nil {
		fmt.Fprintf(r.progress, "Command %s: %s (%s)\n", status, c.Label, elapsed)
	}
	return out, err
}
