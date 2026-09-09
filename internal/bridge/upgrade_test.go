package bridge

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const publishedAlphaCommit = "e22b5b23f11f8f1f9c26ed511416800678ca14fd"

// Build the immutable published-alpha source, not a second build of current
// source. Runtime invocations use only disposable configuration and homes.
func TestPublishedAlphaUpgrade(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_UPGRADE_TESTS") != "1" {
		t.Skip("isolated cross-version build opt-in required")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source location unavailable")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	build := newFixture(t)
	for _, dir := range []string{"home", "tmp", "alpha-source"} {
		must(t, os.MkdirAll(build.path(dir), 0700))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", publishedAlphaCommit)
	archive.Dir = repo
	archive.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + build.path("home"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1"}
	data, err := archive.Output()
	must(t, err)
	reader := tar.NewReader(bytes.NewReader(data))
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		must(t, err)
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		} // Git's commit metadata, never a filesystem entry.
		name := filepath.FromSlash(strings.TrimSuffix(header.Name, "/"))
		path := filepath.Join(build.path("alpha-source"), name)
		if filepath.IsAbs(name) || filepath.Clean(name) != name || !inside(build.path("alpha-source"), path) || path == build.path("alpha-source") {
			t.Fatal("unsafe archive member")
		}
		if header.Typeflag == tar.TypeDir {
			must(t, os.MkdirAll(path, 0700))
			continue
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 0 || header.Size > 10<<20 {
			t.Fatal("unsupported archive member")
		}
		total += header.Size
		if total > 64<<20 {
			t.Fatal("archive exceeds fixture limit")
		}
		must(t, os.MkdirAll(filepath.Dir(path), 0700))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		must(t, err)
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		must(t, copyErr)
		must(t, closeErr)
	}
	goEnv := exec.CommandContext(ctx, "go", "env", "GOCACHE", "GOMODCACHE")
	goEnv.Dir = repo
	cacheData, err := goEnv.Output()
	must(t, err)
	caches := strings.Split(strings.TrimSpace(string(cacheData)), "\n")
	if len(caches) != 2 {
		t.Fatal("Go caches unavailable")
	}
	binaries := map[string]string{}
	for _, version := range []string{"alpha", "current"} {
		binary := build.path("agent-bridge-" + version)
		command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", binary, "./cmd/agent-bridge")
		command.Dir = repo
		if version == "alpha" {
			command.Dir = build.path("alpha-source")
		}
		command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + build.path("home"), "TMPDIR=" + build.path("tmp"), "GOCACHE=" + caches[0], "GOMODCACHE=" + caches[1], "GOENV=off", "GOWORK=off", "CGO_ENABLED=0"}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s build failed: %v\n%s", version, err, output)
		}
		binaries[version] = binary
	}
	for name, setup := range map[string]func(*testing.T) *fixture{
		"raw": newFixture, "instructions": instructionsFixture, "strict-skill": strictSkillFixture,
		"mcp": mcpFixture, "agent": agentFixture, "startup-hook": hooksFixture, "plugin": pluginFixture,
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			for _, dir := range []string{"home", "tmp"} {
				must(t, os.MkdirAll(f.path(dir), 0700))
			}
			nativeRun(t, f, binaries["alpha"], "sync", f.path("config.json"))
			p := f.plan()
			if p.HasConflicts() {
				t.Fatal("published alpha baseline conflicted on upgrade")
			}
			for _, item := range p.Items {
				if len(item.Writes) != 0 {
					t.Fatal("upgrade rewrites existing native data", item.Key)
				}
			}
			nativeRun(t, f, binaries["current"], "sync", f.path("config.json"))
			edit := map[string][4]string{
				"raw":          {"AGENTS.md", "CLAUDE.md", "one", "upgraded"},
				"instructions": {"codex-source", "claude-source", "Shared guidance.", "Upgraded guidance."},
				"strict-skill": {"codex-skill/SKILL.md", "claude-skill/SKILL.md", "A portable demo.", "An upgraded demo."},
				"mcp":          {"codex.toml", "claude.json", "demo-server", "upgraded-server"},
				"agent":        {"codex-source", "claude-source", "Review carefully.", "Review upgraded code."},
				"startup-hook": {"codex-source", "claude-source", "/usr/bin/true", "/usr/bin/false"},
				"plugin":       {"codex-plugin/.codex-plugin/plugin.json", "claude-plugin/.claude-plugin/plugin.json", "1.0.0", "1.1.0"},
			}[name]
			if !strings.Contains(f.read(edit[0]), edit[2]) {
				t.Fatal("edit fixture missing")
			}
			f.write(edit[0], strings.Replace(f.read(edit[0]), edit[2], edit[3], 1))
			nativeRun(t, f, binaries["current"], "sync", f.path("config.json"))
			if !strings.Contains(f.read(edit[1]), edit[3]) {
				t.Fatal("new writer did not propagate post-upgrade edit")
			}
			// Downgrade check applies only to this unchanged legacy feature set.
			nativeRun(t, f, binaries["alpha"], "sync", f.path("config.json"))
			f.write(edit[1], strings.Replace(f.read(edit[1]), edit[3], edit[2], 1))
			nativeRun(t, f, binaries["alpha"], "sync", f.path("config.json"))
			if !strings.Contains(f.read(edit[0]), edit[2]) {
				t.Fatal("old writer did not propagate reverse edit")
			}
			nativeRun(t, f, binaries["current"], "sync", f.path("config.json"))
			if f.plan().HasConflicts() {
				t.Fatal("legacy subset downgrade damaged baseline")
			}
			if _, err := History(f.path("config.json")); err != nil {
				t.Fatal("legacy history became unreadable", err)
			}
		})
	}
	t.Run("old-journal-recovery", func(t *testing.T) {
		f := newFixture(t)
		for _, dir := range []string{"home", "tmp"} {
			must(t, os.MkdirAll(f.path(dir), 0700))
		}
		nativeRun(t, f, binaries["alpha"], "sync", f.path("config.json"))
		history, err := History(f.path("config.json"))
		must(t, err)
		if len(history) != 1 {
			t.Fatal(history)
		}
		// Recreate the pending marker to exercise the actual old writer's
		// journal. This simulates interruption, not a power-loss test.
		must(t, writeJSON(pendingPath(f.c), Pending{history[0].Transaction}))
		nativeRun(t, f, binaries["current"], "recover", f.path("config.json"))
		f.expect("CLAUDE.md", "one")
		f.missing("AGENTS.md")
		f.missing("state/manifest.json")
		nativeRun(t, f, binaries["current"], "sync", f.path("config.json"))
		f.expect("AGENTS.md", "one")
	})
}
