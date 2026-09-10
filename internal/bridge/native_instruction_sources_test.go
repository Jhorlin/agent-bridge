package bridge

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestNativeClaudeAlternateInstructionSources(t *testing.T) {
	for _, mode := range []string{"alternate-only", "both"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			f.write("CLAUDE.md", "")
			if mode == "both" {
				f.write("CLAUDE.md", "BRIDGE_ROOT_INSTRUCTION_SOURCE\n")
			}
			f.write(".claude/CLAUDE.md", "BRIDGE_ALTERNATE_INSTRUCTION_SOURCE\n")
			server, requests := nativePluginFixtureProvider(t)
			nativeRunEnvironment(t, f, tools["claude"], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user,project", "Return fixture complete.")
			root, alternate := false, false
			for len(requests) > 0 {
				body := <-requests
				if mode == "both" && strings.Contains(body, "BRIDGE_ROOT_INSTRUCTION_SOURCE") && strings.Index(body, "BRIDGE_ROOT_INSTRUCTION_SOURCE") > strings.Index(body, "BRIDGE_ALTERNATE_INSTRUCTION_SOURCE") {
					t.Fatal("native instruction source order changed")
				}
				root = root || strings.Contains(body, "BRIDGE_ROOT_INSTRUCTION_SOURCE")
				alternate = alternate || strings.Contains(body, "BRIDGE_ALTERNATE_INSTRUCTION_SOURCE")
			}
			t.Logf("root loaded: %v; alternate loaded: %v", root, alternate)
			if !alternate || root != (mode == "both") {
				t.Fatal("native alternate instruction loading changed")
			}
		})
	}
}

func TestNativeInstructionSetProjectBudget(t *testing.T) {
	for _, raised := range []bool{false, true} {
		t.Run(fmt.Sprintf("raised=%v", raised), func(t *testing.T) {
			f := instructionSetFixture(t, false)
			tools := nativeTools(t, f)
			f.write("CLAUDE.md", "BRIDGE_LARGE_ROOT\n")
			f.write(".claude/CLAUDE.md", strings.Repeat("Instruction fixture.\n", 2200)+"BRIDGE_INSTRUCTION_TAIL\n")
			f.apply()
			must(t, exec.Command("git", "init", "--quiet", f.dir).Run())
			if raised {
				f.write(".codex/config.toml", "project_doc_max_bytes = 131072\n")
			}
			server, requests := nativePluginFixtureProvider(t)
			f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+fmt.Sprintf("\n[features]\nplugins=false\n[projects.%q]\ntrust_level='trusted'\n", f.dir))
			nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
			seenRoot, seenTail := false, false
			for len(requests) > 0 {
				body := <-requests
				seenRoot = seenRoot || strings.Contains(body, "BRIDGE_LARGE_ROOT")
				seenTail = seenTail || strings.Contains(body, "BRIDGE_INSTRUCTION_TAIL")
			}
			if !seenRoot || seenTail != raised {
				t.Fatal("native instruction budget contract changed", seenRoot, seenTail)
			}
		})
	}
}

func TestNativeComposedInstructionSources(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := instructionSetFixture(t, false)
			tools := nativeTools(t, f)
			f.write("CLAUDE.md", "BRIDGE_COMPOSED_ROOT\n")
			f.write(".claude/CLAUDE.md", "BRIDGE_COMPOSED_ALTERNATE\n")
			f.apply()
			f.write("AGENTS.md", strings.Replace(f.read("AGENTS.md"), "BRIDGE_COMPOSED_ALTERNATE", "BRIDGE_COMPOSED_REVERSE_EDIT", 1))
			f.apply()
			server, requests := nativePluginFixtureProvider(t)
			if host == "codex" {
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
				nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
			} else {
				nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user,project", "Return fixture complete.")
			}
			seen := false
			for len(requests) > 0 {
				body := <-requests
				root, alternate := strings.Index(body, "BRIDGE_COMPOSED_ROOT"), strings.Index(body, "BRIDGE_COMPOSED_REVERSE_EDIT")
				if root >= 0 && alternate > root {
					seen = true
				}
				if strings.Contains(body, "BRIDGE_COMPOSED_ALTERNATE") {
					t.Fatal("stale source instructions loaded")
				}
			}
			if !seen {
				t.Fatal("native host did not load both composed instruction sources in order")
			}
		})
	}
}
