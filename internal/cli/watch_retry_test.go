package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type watchMessages chan string

func (w watchMessages) Write(p []byte) (int, error) { w <- string(p); return len(p), nil }
func expectWatchMessage(t *testing.T, messages watchMessages, want string) {
	t.Helper()
	select {
	case message := <-messages:
		if !strings.Contains(message, want) || strings.Contains(message, "PRIVATE") {
			t.Fatalf("unexpected watch diagnostic: %s", message)
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("no watch %s message", want)
	}
}

func TestWatchRetriesIncompleteInputs(t *testing.T) {
	for _, invalid := range []string{"profile", "mcp"} {
		t.Run(invalid, func(t *testing.T) {
			dir, profile := setup(t)
			path := profile
			if invalid == "mcp" {
				if err := os.WriteFile(profile, []byte(`{"version":1,"stateDir":"state","resources":[{"id":"tools","kind":"mcp-config","scope":"project","claude":"mcp.json","codex":"codex.toml","servers":["demo"],"allowReformat":true}]}`), 0600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(dir, "mcp.json")
				if err := os.WriteFile(path, []byte(`{"mcpServers":{"demo":{"command":"fixture"}}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"PRIVATE":"incomplete`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			messages := make(watchMessages, 16)
			done := make(chan int, 1)
			go func() { done <- Run(ctx, []string{"watch", profile, "--apply"}, io.Discard, messages) }()
			expectWatchMessage(t, messages, "Watch paused")
			if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
				t.Fatal("invalid input created state")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			expectWatchMessage(t, messages, "planning resumed")
			if invalid == "profile" {
				waitFile(t, filepath.Join(dir, "AGENTS.md"), "one")
			} else {
				// The native output must be produced after the source becomes valid.
				deadline := time.Now().Add(4 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(dir, "codex.toml")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("no MCP output")
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			cancel()
			select {
			case code := <-done:
				if code != 0 {
					t.Fatal(code)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("watch did not cancel")
			}
		})
	}
}

func TestWatchUnreadableInputDoesNotSpam(t *testing.T) {
	dir, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2100*time.Millisecond)
	defer cancel()
	var diagnostics bytes.Buffer
	if code := Run(ctx, []string{"watch", filepath.Join(dir, "missing.json")}, io.Discard, &diagnostics); code != 0 {
		t.Fatal(code)
	}
	if strings.Count(diagnostics.String(), "Watch paused") != 1 {
		t.Fatal(diagnostics.String())
	}
}
