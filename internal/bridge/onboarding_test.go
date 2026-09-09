package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func onboardingRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDiscoveryScopesAndPrivacy(t *testing.T) {
	root := onboardingRoot(t)
	for _, name := range []string{"CLAUDE.md", ".claude/CLAUDE.md", ".claude/settings.json", ".mcp.json", ".claude/agents/reviewer.md", ".codex/agents/writer.toml", ".claude/skills/demo/SKILL.md", ".claude.json", ".codex/auth.json"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("PRIVATE invalid native content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []string{"global", "project"} {
		report, err := Discover(root, scope)
		if err != nil {
			t.Fatal(err)
		}
		want := 5
		if scope == "project" {
			want = 6
		}
		if !report.ReadOnly || len(report.Candidates) != want {
			t.Fatalf("unexpected report: %+v", report)
		}
		previous := ""
		for _, candidate := range report.Candidates {
			if candidate.Portable || candidate.AllowReformat || candidate.Scope != scope || candidate.ID <= previous {
				t.Fatalf("unsafe candidate: %+v", candidate)
			}
			previous = candidate.ID
			if candidate.ID == "instructions" {
				want := filepath.Join(root, "CLAUDE.md")
				if scope == "global" {
					want = filepath.Join(root, ".claude", "CLAUDE.md")
				}
				if candidate.Claude != want {
					t.Fatal(candidate.Claude)
				}
			}
		}
		data, _ := json.Marshal(report)
		if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), "auth.json") {
			t.Fatal("discovery exposed contents or credentials")
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-bridge")); !os.IsNotExist(err) {
		t.Fatal("discovery created state")
	}
}

func TestDiscoveryRejectsUnsafeRootsAndCandidates(t *testing.T) {
	root := onboardingRoot(t)
	if _, err := Discover(root, "invalid"); err == nil {
		t.Fatal("accepted invalid scope")
	}
	if _, err := Discover(filepath.Join(root, "missing"), "global"); err == nil {
		t.Fatal("accepted absent root")
	}
	if err := os.Symlink(root, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(filepath.Join(root, "alias"), "global"); err == nil {
		t.Fatal("followed root link")
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, "project"); err == nil {
		t.Fatal("accepted candidate link")
	}
}

func TestInitProfileExclusiveAndEmpty(t *testing.T) {
	root := onboardingRoot(t)
	path := filepath.Join(root, "bridge.json")
	if err := InitProfile(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile not private")
	}
	c, err := LoadConfig(path)
	if err != nil || len(c.Resources) != 0 {
		t.Fatalf("invalid empty profile: %+v %v", c, err)
	}
	if err := InitProfile(path); err == nil {
		t.Fatal("overwrote existing profile")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("existing profile changed")
	}
	if _, err := os.Stat(c.StateDir); !os.IsNotExist(err) {
		t.Fatal("init created state")
	}
}
