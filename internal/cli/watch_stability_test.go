package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchWaitsForSecondObservation(t *testing.T) {
	dir, profile := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(watchMessages, 16)
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, messages, io.Discard) }()
	// The first plan is reported, but is not yet applied.
	expectWatchMessage(t, messages, "pending")
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("watch wrote on its first observation")
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("finished edit"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "finished edit")
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not cancel")
	}
}
