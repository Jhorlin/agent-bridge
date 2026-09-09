package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConventionsWatcherDiscoversWithoutProfileEdits(t *testing.T) {
	dir, profile := setup(t)
	if err := os.WriteFile(profile, []byte(`{"version":1,"stateDir":"state","conventions":{"root":"."},"resources":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, io.Discard, io.Discard) }()
	defer func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("watch exit %d", code)
			}
		case <-time.After(3 * time.Second):
			t.Error("watch failed to stop")
		}
	}()
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
	for _, side := range []string{"CLAUDE.md", "AGENTS.md"} {
		sub := filepath.Join(dir, side+"-fixture", "nested")
		if err := os.MkdirAll(sub, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, side), []byte("new rules"), 0600); err != nil {
			t.Fatal(err)
		}
		other := "CLAUDE.md"
		if side == other {
			other = "AGENTS.md"
		}
		waitFile(t, filepath.Join(sub, other), "new rules")
		if err := os.WriteFile(filepath.Join(sub, other), []byte("reverse edit"), 0600); err != nil {
			t.Fatal(err)
		}
		waitFile(t, filepath.Join(sub, side), "reverse edit")
	}
}

func TestInitConventionsCLI(t *testing.T) {
	dir, _ := setup(t)
	profile := filepath.Join(dir, "new.json")
	if code := Run(context.Background(), []string{"init", profile, "--conventions"}, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if code := Run(context.Background(), []string{"init", profile, "--conventions"}, io.Discard, io.Discard); code == 0 {
		t.Fatal("overwrote existing profile")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("init wrote instructions")
	}
	if code := Run(context.Background(), []string{"sync", profile}, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
}
