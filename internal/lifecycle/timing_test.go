package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

type runnerFunc func(context.Context, command) ([]byte, error)

func (f runnerFunc) run(ctx context.Context, c command) ([]byte, error) { return f(ctx, c) }

func TestCommandTimingsPreserveOutputAndErrors(t *testing.T) {
	failure := errors.New("fixture gate failed")
	for _, tc := range []struct {
		name, status string
		err          error
	}{{"success", "passed", nil}, {"failure", "failed", failure}, {"cancelled", "failed", context.Canceled}} {
		t.Run(tc.name, func(t *testing.T) {
			mock := clock.NewMock()
			started := mock.Now()
			var log, progress bytes.Buffer
			r := timedRunner{clock: mock, log: &log, progress: &progress, next: runnerFunc(func(ctx context.Context, c command) ([]byte, error) {
				if ctx != t.Context() || c.Label != "full Android sweep" || c.Name != "fixture" {
					t.Fatal("timing changed command or context")
				}
				mock.Add(2350 * time.Millisecond)
				return []byte("native reporter output\n"), tc.err
			})}
			out, err := r.run(t.Context(), command{Name: "fixture", Label: "full Android sweep"})
			if string(out) != "native reporter output\n" || err != tc.err {
				t.Fatalf("timing changed result: %q, %v", out, err)
			}
			for _, want := range []string{"Command started: " + started.UTC().Format(time.RFC3339Nano), "Command finished: " + mock.Now().UTC().Format(time.RFC3339Nano), "elapsed=2.35s status=" + tc.status} {
				if !strings.Contains(log.String(), want) {
					t.Fatalf("missing %q in timing log: %s", want, log.String())
				}
			}
			if want := "Command " + tc.status + ": full Android sweep (2.35s)\n"; progress.String() != want {
				t.Fatalf("progress=%q, want %q", progress.String(), want)
			}
		})
	}
}

func TestUnlabelledCommandTimingDoesNotFloodProgress(t *testing.T) {
	var log, progress bytes.Buffer
	r := timedRunner{clock: clock.NewMock(), log: &log, progress: &progress, next: runnerFunc(func(context.Context, command) ([]byte, error) { return nil, nil })}
	if _, err := r.run(t.Context(), command{Name: "git"}); err != nil {
		t.Fatal(err)
	}
	if log.Len() == 0 || progress.Len() != 0 {
		t.Fatalf("unlabelled timing missing or leaked to progress: log=%q progress=%q", log.String(), progress.String())
	}
}
