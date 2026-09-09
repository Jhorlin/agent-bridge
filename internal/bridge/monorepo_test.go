package bridge

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestConventionFeatureDirectoryCandidates(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write("plain/deep/source.go", "ordinary source")
	f.write("nested/.claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\nHelp.\n")
	f.write("mcp/.mcp.json", `{"mcpServers":{}}`)
	f.write("ignored/.git/HEAD", "nested repository")
	f.write("ignored/.claude/settings.json", "invalid")
	reloadConventions(t, f)
	dirs, err := conventionFeatureDirs(f.c)
	must(t, err)
	if !reflect.DeepEqual(dirs, []string{f.dir, f.path("mcp"), f.path("nested")}) {
		t.Fatalf("probing ordinary directories: %v", dirs)
	}
	f.apply()
	must(t, os.RemoveAll(f.path("nested/.claude")))
	must(t, os.RemoveAll(f.path("nested/.agents")))
	reloadConventions(t, f)
	if len(f.c.Resources) != 3 {
		t.Fatal("missing tracked collection was dropped")
	}
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("missing entry points accepted")
	}
}

func TestConventionFeatureCandidatesRetainUnsafeInputs(t *testing.T) {
	for _, name := range []string{".claude", ".codex", ".agents", ".mcp.json", ".agent-bridge-plugins"} {
		t.Run(name, func(t *testing.T) {
			f := allConventionFixture(t, "project")
			f.write("nested/ordinary.txt", "fixture")
			reloadConventions(t, f)
			must(t, os.Symlink(f.dir, f.path("nested/"+name)))
			dirs, err := conventionFeatureDirs(f.c)
			must(t, err)
			if !reflect.DeepEqual(dirs, []string{f.dir, f.path("nested")}) {
				t.Fatal("unsafe candidate hidden")
			}
			if _, err := LoadConfig(f.path("config.json")); err == nil {
				t.Fatal("unsafe native root accepted")
			}
		})
	}
}

func TestConventionCollectionDocumentation(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".claude/skills/README.md", "Collection index; not a skill.")
	f.write(".claude/skills/LICENSE", "Fixture license")
	f.write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\nBody.\n")
	reloadConventions(t, f)
	f.apply()
	f.missing(".agents/skills/README.md")
	f.missing(".agents/skills/LICENSE")
	if !strings.Contains(f.read(".agents/skills/demo/SKILL.md"), "Body.") {
		t.Fatal("skill missing")
	}
	must(t, os.Remove(f.path(".claude/skills/README.md")))
	must(t, os.Symlink(f.path("CLAUDE.md"), f.path(".claude/skills/README.md")))
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("documentation symlink accepted")
	}
}

func TestInformationalSkillMetadataRoundTrip(t *testing.T) {
	for _, invocation := range []bool{false, true} {
		f := strictSkillFixture(t)
		f.raw.Resources[0].TranslateSkillInvocation = invocation
		f.load()
		front := "---\nname: demo\ndescription: Demo.\nlicense: MIT\ncompatibility: Requires a local fixture executable.\nmetadata:\n  author: Example\n  version: '1.0'\n"
		if invocation {
			front += "disable-model-invocation: true\n"
		}
		f.write("claude-skill/SKILL.md", front+"---\nBody.\n")
		f.apply()
		for _, marker := range []string{"license: MIT", "compatibility:", "author: Example", "version:"} {
			if !strings.Contains(f.read("codex-skill/SKILL.md"), marker) {
				t.Fatal("informational metadata lost")
			}
		}
		f.write("codex-skill/SKILL.md", strings.Replace(f.read("codex-skill/SKILL.md"), "author: Example", "author: Revised", 1))
		f.apply()
		if !strings.Contains(f.read("claude-skill/SKILL.md"), "author: Revised") {
			t.Fatal("reverse metadata missing")
		}
		f.apply()
		if invocation && !strings.Contains(f.read("codex-skill/agents/openai.yaml"), "false") {
			t.Fatal("invocation changed")
		}
	}
}

func TestInformationalSkillMetadataRejectsUnsafeShapes(t *testing.T) {
	for _, field := range []string{"license: [MIT]", "compatibility: " + strings.Repeat("x", 501), "metadata: [bad]", "metadata:\n  author: [bad]", "metadata:\n  author: &anchor Example", "metadata:\n  author: Example\n  author: Other", "metadata:\n  author: !!binary b2s="} {
		f := strictSkillFixture(t)
		f.write("claude-skill/SKILL.md", "---\nname: demo\ndescription: Demo.\n"+field+"\n---\nBody.\n")
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("invalid metadata accepted")
		}
		f.missing("codex-skill/SKILL.md")
	}
}

func TestConventionScopedRulesWarning(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".claude/rules/private-rule.md", "PRIVATE_RULE_CONTENT")
	reloadConventions(t, f)
	if len(f.c.ConventionWarnings) == 0 {
		t.Fatal("scoped rules silently ignored")
	}
	for _, note := range f.c.ConventionWarnings {
		if strings.Contains(note, "PRIVATE_RULE_CONTENT") {
			t.Fatal("private warning")
		}
	}
}

func TestConventionAlternateFileExclusions(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write("services/one/CLAUDE.md", "service")
	f.write("services/one/.claude/CLAUDE.md", "alternate")
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","features":["instructions"],"exclude":["services/one/CLAUDE.md","services/one/.claude/CLAUDE.md"]},"resources":[]}`)
	reloadConventions(t, f)
	f.apply()
	f.missing("services/one/AGENTS.md")
}

func TestDiagnosticPathsAvoidNativeDiscovery(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".mcp.json", "unparseable native config")
	paths, err := DiagnosticProtectedPaths(f.path("config.json"))
	must(t, err)
	found := false
	for _, p := range paths {
		if p == f.dir {
			found = true
		}
	}
	if !found {
		t.Fatal("future project components not protected")
	}
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("fixture should block native discovery")
	}
	g := allConventionFixture(t, "global")
	paths, err = DiagnosticProtectedPaths(g.path("config.json"))
	must(t, err)
	for _, p := range paths {
		if p == g.dir {
			t.Fatal("global home blanket would disable normal logs")
		}
	}
	for _, name := range []string{".claude", ".codex", ".agents", ".agent-bridge-plugins", ".claude.json"} {
		found = false
		for _, p := range paths {
			if p == g.path(name) {
				found = true
			}
		}
		if !found {
			t.Fatal("global component root not protected")
		}
	}
}

func TestConventionInstructionCodeFences(t *testing.T) {
	for _, input := range []string{"# Rules\n\n```sh\npnpm --filter @example/client build\n```\n", "~~~text\n@example/client -> library\n~~~~\n", "   ```text\n@sample/name\n   ```\n", "Run `tool @example/client` or ``tool `literal` @example/client``.\n"} {
		f := allConventionFixture(t, "project")
		f.write("CLAUDE.md", input)
		reloadConventions(t, f)
		f.apply()
		f.expect("AGENTS.md", input)
		f.write("CLAUDE.md", input+"Read @docs/private.md\n")
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("real import accepted")
		}
	}
	for _, input := range []string{"```\n@docs/private.md\n", "```\n~~~\n@docs/private.md\n", "```text\ntext\n``` trailing\n@docs/private.md\n"} {
		f := allConventionFixture(t, "project")
		f.write("CLAUDE.md", input)
		reloadConventions(t, f)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("ambiguous fence accepted")
		}
	}
}

func TestConventionSkillHostExpansionRejected(t *testing.T) {
	for _, marker := range []string{"$ARGUMENTS", "${CLAUDE_SKILL_DIR}", "!`command`", "$1"} {
		f := allConventionFixture(t, "project")
		f.write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\nUse "+marker+".\n")
		reloadConventions(t, f)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("host-only skill expression accepted")
		}
		f.missing(".agents/skills/demo/SKILL.md")
	}
}
