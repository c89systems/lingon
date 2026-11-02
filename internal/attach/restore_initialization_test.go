package attach

import (
	"sync"
	"testing"
)

// Render callbacks may inspect a client before its Run goroutine has started.
func TestRestoreModeHonorsConfigurationBeforeRun(t *testing.T) {
	c := &Client{Restore: true}
	const readers = 32
	start := make(chan struct{})
	results := make(chan bool, readers)
	var workers sync.WaitGroup
	for range readers {
		workers.Go(func() { <-start; results <- c.restoreEnabled() })
	}
	close(start)
	workers.Wait()
	close(results)
	for enabled := range results {
		if !enabled {
			t.Fatal("restore configuration was lost before Run initialized the client")
		}
	}
}

func TestRestoreInitializationPreservesSharedMode(t *testing.T) {
	mode := newRestoreMode(false)
	first := &Client{Restore: true, restoreMode: mode}
	second := &Client{restoreMode: mode}
	if first.restoreEnabled() || second.restoreEnabled() {
		t.Fatal("local config replaced the supplied shared mode")
	}
	mode.Store(true)
	if !first.restoreEnabled() || !second.restoreEnabled() {
		t.Fatal("clients lost the shared mode")
	}
}
