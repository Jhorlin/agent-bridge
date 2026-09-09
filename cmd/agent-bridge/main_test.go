//go:build darwin || linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if profile := os.Getenv("AGENT_BRIDGE_EXEC_HELPER_PROFILE"); profile != "" {
		os.Args = []string{"agent-bridge", "watch", profile, "--apply"}
		main()
		return
	}
	os.Exit(m.Run())
}

func TestWatcherProcessSignalsAndRestart(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			profile := filepath.Join(root, "bridge.json")
			data, err := json.Marshal(map[string]any{"version": 1, "stateDir": "state", "coordinationDir": "coordination", "resources": []any{map[string]any{"id": "rules", "kind": "portable-file", "scope": "project", "claude": "CLAUDE.md", "codex": "AGENTS.md"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(profile, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"first sync", "after restart"} {
				if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0])
				cmd.Dir = root
				cmd.Env = []string{"AGENT_BRIDGE_EXEC_HELPER_PROFILE=" + profile}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				finished := make(chan error, 1)
				go func() { finished <- cmd.Wait() }()
				stopped := false
				defer func() {
					if !stopped {
						cmd.Process.Kill()
						<-finished
					}
				}()
				deadline := time.Now().Add(8 * time.Second)
				for {
					data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
					if err == nil && string(data) == text {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("watcher did not synchronize fixture")
					}
					time.Sleep(20 * time.Millisecond)
				}
				if err := cmd.Process.Signal(signal); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-finished:
					stopped = true
					if err != nil {
						t.Fatalf("watcher did not exit cleanly: %v", err)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("watcher ignored shutdown signal")
				}
				for _, name := range []string{"state/sync.lock", "coordination/sync.lock", "state/pending.json"} {
					if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
						t.Fatalf("shutdown retained %s: %v", name, err)
					}
				}
			}
		})
	}
}
