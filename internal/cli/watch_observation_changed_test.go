package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

// Run asks the context for its observer immediately before planning and again
// before constructing Apply options. The fourth lookup is therefore between
// the second stable plan and Apply. Editing there deterministically exercises
// the real stale-observation path without a production test hook or a save race.
type editBeforeWatchApplyContext struct {
	context.Context
	lookups int
	edit    func()
}

func (c *editBeforeWatchApplyContext) Value(key any) any {
	if _, ok := key.(observerKey); ok {
		c.lookups++
		if c.lookups == 4 {
			c.edit()
		}
	}
	return c.Context.Value(key)
}

func TestWatchRetriesObservationChangedDuringMCPApply(t *testing.T) {
	dir, profile := setup(t)
	if err := os.WriteFile(profile, []byte(`{"version":1,"stateDir":"state","resources":[{"id":"tools","kind":"mcp-config","scope":"project","claude":"mcp.json","codex":"codex.toml","servers":["demo"],"allowReformat":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	source, target := filepath.Join(dir, "mcp.json"), filepath.Join(dir, "codex.toml")
	writeSource := func(command string) error {
		return os.WriteFile(source, []byte(`{"mcpServers":{"demo":{"command":"`+command+`"}}}`), 0600)
	}
	if err := writeSource("original-fixture"); err != nil {
		t.Fatal(err)
	}
	events := make(chan diagnostics.Event, 32)
	staleTargetAbsent := make(chan bool, 1)
	base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	base = context.WithValue(base, observerKey{}, diagnostics.Observer(func(e diagnostics.Event) {
		if e.Stage == "plan" && e.Code == "observation_changed" {
			_, err := os.Stat(target)
			staleTargetAbsent <- os.IsNotExist(err)
		}
		events <- e
	}))
	var editErr error
	ctx := &editBeforeWatchApplyContext{Context: base, edit: func() { editErr = writeSource("replacement-fixture") }}
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, io.Discard, io.Discard) }()
	waitEvent := func(stage, code string) {
		t.Helper()
		for {
			select {
			case e := <-events:
				if e.Stage == stage && e.Code == code {
					return
				}
			case exit := <-done:
				t.Fatalf("watch exited before %s/%s: %d", stage, code, exit)
			case <-base.Done():
				t.Fatalf("watch did not emit %s/%s", stage, code)
			}
		}
	}
	waitEvent("plan", "observation_changed")
	if !<-staleTargetAbsent {
		t.Fatal("stale plan wrote the native target")
	}
	waitEvent("commit", "ok")
	data, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "replacement-fixture") || strings.Contains(string(data), "original-fixture") {
		t.Fatalf("watch failed to synchronize the fresh replacement: %v", err)
	}
	// The same watcher must remain alive for a later independent edit.
	if err := writeSource("later-fixture"); err != nil {
		t.Fatal(err)
	}
	waitEvent("commit", "ok")
	data, err = os.ReadFile(target)
	if err != nil || !strings.Contains(string(data), "later-fixture") {
		t.Fatalf("watch failed to continue after stale-observation recovery: %v", err)
	}
	cancel()
	select {
	case exit := <-done:
		if exit != 0 || editErr != nil {
			t.Fatalf("watch exit=%d; injected edit error=%v", exit, editErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not cancel")
	}
}
