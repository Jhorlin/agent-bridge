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

func TestAllFeatureConventionWatcher(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			dir, _ := setup(t)
			profile := filepath.Join(dir, "all.json")
			args := []string{"init", profile, "--conventions"}
			if scope == "global" {
				args = []string{"init", profile, "--global", dir}
			}
			if code := Run(context.Background(), args, io.Discard, io.Discard); code != 0 {
				t.Fatal(code)
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
					t.Error("watch did not stop")
				}
			}()
			write := func(path, text string) {
				t.Helper()
				path = filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\nWATCH_SKILL_BODY\n")
			write(".claude/agents/reviewer.md", "---\nname: reviewer\ndescription: Review.\n---\nWATCH_AGENT_BODY\n")
			write(".claude/settings.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/fixture/stop","timeout":5}]}]}}`)
			write(".agent-bridge-plugins/codex/demo/.codex-plugin/plugin.json", `{"name":"demo"}`)
			mcp := ".mcp.json"
			if scope == "global" {
				mcp = ".claude.json"
			}
			write(mcp, `{"mcpServers":{"one":{"command":"first"}}}`)
			waitContains := func(path, want string) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					data, _ := os.ReadFile(filepath.Join(dir, path))
					if strings.Contains(string(data), want) {
						return
					}
					time.Sleep(25 * time.Millisecond)
				}
				t.Fatalf("watch failed for %s", path)
			}
			waitContains(".agents/skills/demo/SKILL.md", "WATCH_SKILL_BODY")
			waitContains(".codex/agents/reviewer.toml", "WATCH_AGENT_BODY")
			waitContains(".codex/hooks.json", "/fixture/stop")
			waitContains(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json", "demo")
			waitContains(".codex/config.toml", "first")
			write(mcp, `{"mcpServers":{"one":{"command":"first"},"two":{"command":"WATCH_LATE_SERVER"}}}`)
			waitContains(".codex/config.toml", "WATCH_LATE_SERVER")
			write(".codex/agents/reviewer.toml", "name = 'reviewer'\ndescription = 'Review.'\ndeveloper_instructions = 'WATCH_REVERSE_AGENT'\n")
			waitContains(".claude/agents/reviewer.md", "WATCH_REVERSE_AGENT")
		})
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
