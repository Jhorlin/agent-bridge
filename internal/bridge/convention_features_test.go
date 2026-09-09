package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func allConventionFixture(t *testing.T, scope string) *fixture {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"`+scope+`","features":["instructions","skills","agents","hooks","mcp","plugins"]},"resources":[]}`)
	return f
}

func TestAllConventionsAdditionalBoundaries(t *testing.T) {
	t.Run("explicit skill exception keeps identity and adapter", func(t *testing.T) {
		f := newFixture(t)
		f.raw.Resources = []resourceInput{{ID: "existing-skill", Kind: "skill-directory", Scope: "project", Portable: true, Claude: ".claude/skills/LegacySkill", Codex: ".agents/skills/LegacySkill"}}
		f.write(".claude/skills/LegacySkill/SKILL.md", "Existing raw skill content")
		f.load()
		f.apply()
		f.raw.Conventions = &Conventions{Root: ".", Features: []string{"skills"}}
		f.load()
		if len(f.c.Resources) != 1 || f.c.Resources[0].ID != "existing-skill" || f.c.Resources[0].TranslateSkillInvocation {
			t.Fatal("explicit exception replaced")
		}
		f.apply()
	})
	t.Run("global root cannot be implicit", func(t *testing.T) {
		f := newFixture(t)
		if err := InitGlobalConventionProfile(f.path("global.json"), ""); err == nil {
			t.Fatal("implicit root accepted")
		}
		f.missing("global.json")
	})
	t.Run("history across member growth refuses without writes", func(t *testing.T) {
		f := allConventionFixture(t, "project")
		f.write(".mcp.json", `{"mcpServers":{"one":{"command":"first"}}}`)
		reloadConventions(t, f)
		tx := initialHistory(t, f)
		id := ""
		for _, r := range f.c.Resources {
			if r.Kind == "mcp-config" {
				id = r.ID
			}
		}
		f.write(".mcp.json", `{"mcpServers":{"one":{"command":"first"},"two":{"command":"second"}}}`)
		reloadConventions(t, f)
		f.apply()
		before := auditTree(t, f.dir)
		_, err := ReviewHistory(f.path("config.json"), HistoryChoice{tx, id, "codex", "after"})
		if err == nil {
			t.Fatal("pre-growth history accepted")
		}
		contains(t, err, "identity")
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("blocked history changed files")
		}
	})
	t.Run("excluded components are not read", func(t *testing.T) {
		f := allConventionFixture(t, "project")
		f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","features":["skills","agents","plugins"],"exclude":[".claude/skills/private",".claude/agents/private.md",".agent-bridge-plugins/claude/private"]},"resources":[]}`)
		f.write(".agent-bridge-plugins/claude/private/.claude-plugin/plugin.json", "invalid private data")
		f.write(".agent-bridge-plugins/claude/private/.mcp.json", "invalid private data")
		f.write(".agents/skills/private/SKILL.md", "private counterpart")
		must(t, os.MkdirAll(f.path(".claude/skills"), 0700))
		must(t, os.Symlink(f.path(".agents/skills/private"), f.path(".claude/skills/private")))
		f.write(".claude/agents/private.md", "invalid private data")
		reloadConventions(t, f)
		if len(f.c.Resources) != 0 {
			t.Fatal("excluded components enrolled")
		}
	})
	t.Run("global override blocks", func(t *testing.T) {
		f := allConventionFixture(t, "global")
		f.write(".claude/CLAUDE.md", "rules")
		f.write(".codex/AGENTS.override.md", "shadow rules")
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatal("shadowing override accepted")
		}
		f.missing(".codex/AGENTS.md")
	})
	t.Run("empty hooks do not enroll unrelated settings", func(t *testing.T) {
		f := allConventionFixture(t, "global")
		f.write(".claude/settings.json", `{"hooks":{},"permissions":{"deny":["Bash(rm:*)"]}}`)
		reloadConventions(t, f)
		if len(f.c.Resources) != 0 {
			t.Fatal("empty hooks enrolled")
		}
	})
	t.Run("portable plugin layout", func(t *testing.T) {
		f := allConventionFixture(t, "project")
		f.write(".agent-bridge-plugins/codex/demo/plugin.json", `{"name":"demo","version":"1.0.0"}`)
		f.write(".agent-bridge-plugins/codex/demo/skills/helper/SKILL.md", "---\nname: helper\ndescription: Help.\n---\nHelp.\n")
		reloadConventions(t, f)
		f.apply()
		f.missing(".agent-bridge-plugins/codex/demo/.codex-plugin/plugin.json")
		if !strings.Contains(f.read(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json"), "demo") {
			t.Fatal("portable plugin not translated")
		}
		reloadConventions(t, f)
		f.apply()
		f.write(".agent-bridge-plugins/codex/demo/.codex-plugin/plugin.json", `{"name":"demo"}`)
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatal("ambiguous layouts accepted")
		}
	})
}

func seedConventionFeatures(f *fixture, global bool) {
	if global {
		f.write(".claude/CLAUDE.md", "global rules")
	}
	f.write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo skill.\n---\nFollow demo steps.\n")
	f.write(".claude/agents/reviewer.md", "---\nname: reviewer\ndescription: Review code.\nmodel: sonnet\n---\nCheck correctness.\n")
	f.write(".claude/settings.json", `{"permissions":{"deny":["Bash(rm:*)"]},"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"/fixture/startup","timeout":5}]}]}}`)
	mcp := ".mcp.json"
	if global {
		mcp = ".claude.json"
	}
	f.write(mcp, `{"oauthAccount":{"token":"PRIVATE_AUTH_SENTINEL"},"mcpServers":{"demo":{"command":"fixture-server","env":{"API_TOKEN":"${API_TOKEN}"}}}}`)
	f.write(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json", `{"name":"demo","version":"1.0.0"}`)
	f.write(".agent-bridge-plugins/claude/demo/skills/helper/SKILL.md", "---\nname: helper\ndescription: Helper skill.\n---\nHelp.\n")
}

func TestAllConventionsProjectAndGlobal(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			f := allConventionFixture(t, scope)
			seedConventionFeatures(f, scope == "global")
			reloadConventions(t, f)
			if len(f.c.Resources) != 6 {
				t.Fatalf("got %d resources, want all six feature kinds", len(f.c.Resources))
			}
			f.apply()
			if !strings.Contains(f.read(".agents/skills/demo/SKILL.md"), "Follow demo steps.") {
				t.Fatal("skill missing")
			}
			if !strings.Contains(f.read(".codex/agents/reviewer.toml"), "Check correctness.") {
				t.Fatal("agent not translated")
			}
			if strings.Contains(f.read(".codex/agents/reviewer.toml"), "sonnet") {
				t.Fatal("host model copied")
			}
			if !strings.Contains(f.read(".codex/hooks.json"), "/fixture/startup") {
				t.Fatal("hook missing")
			}
			if strings.Contains(f.read(".codex/config.toml"), "PRIVATE_AUTH_SENTINEL") {
				t.Fatal("account state copied")
			}
			if !strings.Contains(f.read(".agent-bridge-plugins/codex/demo/.codex-plugin/plugin.json"), "demo") {
				t.Fatal("plugin missing")
			}
			if scope == "global" {
				f.expect(".codex/AGENTS.md", "global rules")
				f.missing("AGENTS.md")
			} else {
				f.expect("AGENTS.md", "one")
			}
			reloadConventions(t, f)
			f.apply()
		})
	}
}

func TestAllConventionsMCPGrowthMergeDeletionAndLocalData(t *testing.T) {
	f := allConventionFixture(t, "global")
	f.write(".claude.json", `{"oauthAccount":{"token":"PRIVATE_AUTH_SENTINEL"},"projects":{"private":{"mcpServers":{"local":{"command":"local-only"}}}},"mcpServers":{"one":{"command":"first"}}}`)
	f.write(".codex/config.toml", "model = 'local-model'\n[mcp_servers.two]\ncommand = 'second'\nenabled = false\n")
	reloadConventions(t, f)
	f.apply()
	if strings.Contains(f.read(".codex/config.toml"), "PRIVATE_AUTH_SENTINEL") || strings.Contains(f.read(".codex/config.toml"), "local-only") {
		t.Fatal("global private state leaked")
	}
	if !strings.Contains(f.read(".claude.json"), "PRIVATE_AUTH_SENTINEL") {
		t.Fatal("account state damaged")
	}
	// Add only to the documented top-level user scope, not per-project state.
	raw, err := snapshot(f.path(".claude.json"))
	must(t, err)
	doc, err := document("claude", raw)
	must(t, err)
	doc["mcpServers"].(map[string]any)["three"] = map[string]any{"command": "third"}
	must(t, writeJSON(f.path(".claude.json"), doc))
	f.write(".codex/config.toml", f.read(".codex/config.toml")+"\n[mcp_servers.four]\ncommand = 'fourth'\n")
	reloadConventions(t, f)
	f.apply()
	if len(f.c.Resources) != 1 || len(f.c.Resources[0].Servers) != 4 {
		t.Fatal("new servers not enrolled")
	}
	if !strings.Contains(f.read(".claude.json"), "fourth") || !strings.Contains(f.read(".codex/config.toml"), "third") {
		t.Fatal("new servers not merged")
	}
	if !strings.Contains(f.read(".codex/config.toml"), "enabled = false") {
		t.Fatal("native policy lost")
	}
	shared := f.read("state/shared/" + f.c.Resources[0].ID)
	if strings.Contains(shared, "PRIVATE_AUTH_SENTINEL") || strings.Contains(shared, "local-model") {
		t.Fatal("private data entered shared store")
	}
	f.write(".claude.json", `{"mcpServers":{}}`)
	f.write(".codex/config.toml", "[mcp_servers]\n")
	reloadConventions(t, f)
	if !f.plan().HasConflicts() {
		t.Fatal("tracked server deletion lost")
	}
}

func TestAllConventionsNewPluginComponentsAndReverseEdits(t *testing.T) {
	f := allConventionFixture(t, "project")
	seedConventionFeatures(f, false)
	reloadConventions(t, f)
	f.apply()
	root := ".agent-bridge-plugins/claude/demo"
	f.write(root+"/agents/reviewer.md", "---\nname: reviewer\ndescription: Review plugin.\nmodel: sonnet\n---\nPlugin review instructions.\n")
	f.write(root+"/.mcp.json", `{"mcpServers":{"one":{"command":"first"}}}`)
	f.write(root+"/commands/demo.md", commandFixture)
	f.write(root+"/hooks/hooks.json", `{"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"/fixture/startup","timeout":5}]}]}}`)
	reloadConventions(t, f)
	f.apply()
	plugin := featureEntry(f.c, "plugins", f.dir, "demo")
	export := ".codex/agents/" + exportedAgentName(plugin.ID, "reviewer") + ".toml"
	if !strings.Contains(f.read(export), "Plugin review instructions.") {
		t.Fatal("plugin agent export missing")
	}
	f.write(export, strings.Replace(f.read(export), "Plugin review instructions.", "Reverse plugin edit.", 1))
	f.write(".agent-bridge-plugins/codex/demo/.mcp.json", `{"mcpServers":{"one":{"command":"first"},"two":{"command":"second"}}}`)
	reloadConventions(t, f)
	f.apply()
	if !strings.Contains(f.read(root+"/agents/reviewer.md"), "Reverse plugin edit.") {
		t.Fatal("reverse export missing")
	}
	if !strings.Contains(f.read(root+"/.mcp.json"), "second") {
		t.Fatal("bundled MCP addition missing")
	}
	if !strings.Contains(f.read(root+"/agents/reviewer.md"), "sonnet") {
		t.Fatal("Claude local model lost")
	}
	reloadConventions(t, f)
	before := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("plugin sync not idempotent")
	}
}

func TestAllConventionsNestedAndReverseSources(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write("nested/.agents/skills/reverse/SKILL.md", "---\nname: reverse\ndescription: Reverse skill.\n---\nReverse skill body.\n")
	f.write("nested/.codex/agents/reviewer.toml", "name = 'reviewer'\ndescription = 'Review'\ndeveloper_instructions = 'Reverse agent body.'\nsandbox_mode = 'read-only'\n")
	f.write("nested/.codex/hooks.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/fixture/stop","timeout":5}]}]}}`)
	f.write("nested/.codex/config.toml", "[mcp_servers.reverse]\ncommand = 'reverse-server'\n")
	f.write("nested/.agent-bridge-plugins/codex/reverse/.codex-plugin/plugin.json", `{"name":"reverse"}`)
	reloadConventions(t, f)
	f.apply()
	if !strings.Contains(f.read("nested/.claude/skills/reverse/SKILL.md"), "Reverse skill body.") {
		t.Fatal("reverse skill missing")
	}
	if strings.Contains(f.read("nested/.claude/agents/reviewer.md"), "read-only") {
		t.Fatal("sandbox policy translated")
	}
	if !strings.Contains(f.read("nested/.claude/settings.json"), "/fixture/stop") {
		t.Fatal("reverse hooks missing")
	}
	if !strings.Contains(f.read("nested/.mcp.json"), "reverse-server") {
		t.Fatal("reverse MCP missing")
	}
	if !strings.Contains(f.read("nested/.agent-bridge-plugins/claude/reverse/.claude-plugin/plugin.json"), "reverse") {
		t.Fatal("reverse package missing")
	}
	reloadConventions(t, f)
	f.apply()
}

func TestAllConventionsSkillInvocationAndDeletion(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\ndisable-model-invocation: true\n---\nBody.\n")
	reloadConventions(t, f)
	f.apply()
	policy := ".agents/skills/demo/agents/openai.yaml"
	if !strings.Contains(f.read(policy), "false") {
		t.Fatal("invocation policy missing")
	}
	f.write(policy, "policy:\n  allow_implicit_invocation: true\n")
	reloadConventions(t, f)
	f.apply()
	if !strings.Contains(f.read(".claude/skills/demo/SKILL.md"), "disable-model-invocation: false") {
		t.Fatal("reverse policy missing")
	}
	must(t, os.Remove(f.path(policy)))
	reloadConventions(t, f)
	if _, err := Plan(f.c); err == nil {
		t.Fatal("deleted policy silently enabled skill")
	}
}

func TestAllConventionsSafetyRollbackRecoveryAndRetirement(t *testing.T) {
	for _, mode := range []string{"credential", "unsupported-hook", "plugin-component", "rollback", "crash", "retire"} {
		t.Run(mode, func(t *testing.T) {
			f := allConventionFixture(t, "project")
			seedConventionFeatures(f, false)
			switch mode {
			case "credential":
				f.write(".mcp.json", `{"mcpServers":{"bad":{"command":"fixture","env":{"TOKEN":"PRIVATE_LITERAL"}}}}`)
			case "unsupported-hook":
				f.write(".claude/settings.json", `{"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"echo unsafe","timeout":5}]}]}}`)
			case "plugin-component":
				f.write(".agent-bridge-plugins/claude/demo/.lsp.json", `{}`)
			}
			reloadConventions(t, f)
			if mode == "credential" || mode == "unsupported-hook" || mode == "plugin-component" {
				_, err := Apply(f.c, Options{})
				if err == nil {
					t.Fatal("unsupported component synchronized")
				}
				f.missing("AGENTS.md")
				return
			}
			if mode == "rollback" {
				_, err := Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
					if index == 3 {
						return errors.New("fixture failure")
					}
					return nil
				}})
				contains(t, err, "rolled back")
				f.missing("AGENTS.md")
				f.missing(".codex/config.toml")
				return
			}
			if mode == "crash" {
				func() {
					defer func() {
						if recover() == nil {
							t.Error("no injected crash")
						}
					}()
					_, _ = Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
						if index == 3 {
							panic("crash")
						}
						return nil
					}})
				}()
				reloadConventions(t, f)
				_, err := Recover(f.c)
				must(t, err)
				f.missing("AGENTS.md")
				return
			}
			f.apply()
			id := featureEntry(f.c, "skills", f.dir, "demo").ID
			r, err := ReviewRetirement(f.path("config.json"), id)
			must(t, err)
			_, err = ApplyRetirement(f.path("config.json"), r.Observation, id)
			must(t, err)
			reloadConventions(t, f)
			for _, r := range f.c.Resources {
				if r.ID == id {
					t.Fatal("retired skill rediscovered")
				}
			}
		})
	}
}

func TestAllConventionsGlobalDoesNotTraverseHomeOrCaches(t *testing.T) {
	f := allConventionFixture(t, "global")
	f.write("project/.claude/skills/private/SKILL.md", "PRIVATE_PROJECT")
	f.write(".claude/plugins/cache/private/.claude-plugin/plugin.json", "PRIVATE_CACHE")
	f.write(".codex/auth.json", "PRIVATE_AUTH")
	f.write(".claude/projects/history.json", "PRIVATE_HISTORY")
	f.write(".claude/CLAUDE.md", "Global rules")
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 || len(f.c.ConventionWarnings) == 0 {
		t.Fatal("global scope/cache boundary not enforced")
	}
	f.apply()
	f.missing("project/.agents")
	f.expect(".codex/auth.json", "PRIVATE_AUTH")
}

func TestAllConventionsLateDiscoveryInvalidatesReview(t *testing.T) {
	f := allConventionFixture(t, "project")
	review, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write(".claude/skills/new/SKILL.md", "---\nname: new\ndescription: New.\n---\nNew body.\n")
	_, err = SyncReviewed(f.path("config.json"), review.Observation)
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatalf("stale review accepted: %v", err)
	}
	f.missing(".agents/skills/new/SKILL.md")
}

func TestAllConventionsInitGlobal(t *testing.T) {
	f := newFixture(t)
	profile := filepath.Join(f.dir, "global.json")
	must(t, InitGlobalConventionProfile(profile, f.dir))
	c, err := LoadConfig(profile)
	must(t, err)
	if c.Conventions.scope() != "global" || len(c.Conventions.Features) != 6 {
		t.Fatal("global init not all-feature")
	}
	if err := InitGlobalConventionProfile(profile, f.dir); err == nil {
		t.Fatal("init overwrote existing profile")
	}
}
