package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type firstWatchOutput chan struct{}

func (w firstWatchOutput) Write(p []byte) (int, error) {
	select {
	case w <- struct{}{}:
	default:
	}
	return len(p), nil
}

func TestWatchRetryHonorsIntervalAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(firstWatchOutput, 1)
	done := make(chan bool, 1)
	go func() {
		blocked := false
		done <- retryWatch(ctx, started, &blocked, 2*time.Second)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not start")
	}
	select {
	case <-done:
		t.Fatal("retry ignored configured interval")
	case <-time.After(1500 * time.Millisecond):
	}
	cancel()
	select {
	case resumed := <-done:
		if resumed {
			t.Fatal("canceled retry resumed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("retry interval delayed cancellation")
	}
}

// Ignoring the configured cadence causes a write after one second instead of
// the requested two; a non-cancelable sleep causes the shutdown assertion to fail.
func TestWatchHonorsProfileIntervalAndCancelsPromptly(t *testing.T) {
	dir, profile := setup(t)
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"version":1`, `"version":1,"watchIntervalSeconds":2`, 1))
	if err := os.WriteFile(profile, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(firstWatchOutput, 1)
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, out, io.Discard) }()
	select {
	case <-out:
	case <-time.After(5 * time.Second):
		t.Fatal("no first observation")
	}
	select {
	case code := <-done:
		t.Fatalf("early exit %d", code)
	case <-time.After(1500 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("watch wrote before configured stable-observation interval")
	}
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("interval delayed cancellation")
	}
}

func TestWatchReloadRestoresDefaultInterval(t *testing.T) {
	dir, profile := setup(t)
	original, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(original), `"version":1`, `"version":1,"watchIntervalSeconds":2`, 1)
	if err := os.WriteFile(profile, []byte(configured), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(firstWatchOutput, 1)
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, out, io.Discard) }()
	select {
	case <-out:
	case <-time.After(5 * time.Second):
		t.Fatal("no initial observation")
	}
	// Atomic editor save of the profile, followed by a changed source, makes the
	// next plan observable. Removing the field must restore the one-second default.
	if err := os.WriteFile(profile+".new", original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(profile+".new", profile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-out:
	case code := <-done:
		t.Fatalf("watch exited %d", code)
	case <-time.After(5 * time.Second):
		t.Fatal("no reloaded observation")
	}
	deadline := time.Now().Add(1700 * time.Millisecond)
	for {
		b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		if string(b) == "two" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("profile reload retained the old two-second interval")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
}
