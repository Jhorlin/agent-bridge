package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setup(t *testing.T) (string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"version": 1, "stateDir": "state", "resources": []map[string]any{{"id": "rules", "kind": "portable-file", "scope": "global", "claude": "CLAUDE.md", "codex": "AGENTS.md"}}})
	if err = os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(dir, "config.json")
}
func waitFile(t *testing.T, file, want string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(file)
		if string(b) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("watch did not synchronize %s", file)
}
func TestWatchApplyAndShutdown(t *testing.T) {
	dir, config := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", config, "--apply"}, io.Discard, io.Discard) }()
	waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("edited via codex"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFile(t, filepath.Join(dir, "CLAUDE.md"), "edited via codex")
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop")
	}
}
func TestSyncConflictExitCode(t *testing.T) {
	dir, config := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"sync", config}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("conflict wrote state")
	}
}
func TestUsageAndUnknownFlags(t *testing.T) {
	for _, args := range [][]string{nil, {"nope", "config"}, {"sync", "config", "--apply"}, {"watch", "config", "--force"}, {"watch", "config", "--apply", "--apply"}} {
		var out bytes.Buffer
		if Run(context.Background(), args, io.Discard, &out) != 1 || out.Len() == 0 {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
func TestPlanAndRecoverCommands(t *testing.T) {
	_, config := setup(t)
	for _, cmd := range []string{"plan", "sync", "recover"} {
		var out bytes.Buffer
		if code := Run(context.Background(), []string{cmd, config}, &out, io.Discard); code != 0 {
			t.Fatalf("%s exit %d", cmd, code)
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("invalid JSON output for %s", cmd)
		}
	}
}
func TestWatchWithoutApplyIsReadOnly(t *testing.T) {
	dir, config := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
	defer cancel()
	if code := Run(ctx, []string{"watch", config}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("read-only watcher wrote state")
	}
}
