package bridge

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestResolutionChoicesFreshnessAndRollback(t *testing.T) {
	for _, mode := range []string{"apply", "stale", "changed-choice", "rollback", "deletion-restore"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.apply()
			f.write("CLAUDE.md", "chosen")
			f.write("AGENTS.md", "other")
			if mode == "deletion-restore" {
				must(t, os.Remove(f.path("AGENTS.md")))
			}
			key := f.c.Resources[0].ID
			choices := map[string]string{key: "claude"}
			before := auditTree(t, f.dir)
			r, err := ReviewResolution(f.path("config.json"), choices)
			must(t, err)
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("review wrote files")
			}
			if mode == "stale" {
				f.write("CLAUDE.md", "later")
			}
			if mode == "changed-choice" {
				choices[key] = "codex"
			}
			if mode == "rollback" {
				_, err = Apply(f.c, Options{ExpectedObservation: r.Observation, Resolutions: choices, BeforeWrite: failSecond})
				contains(t, err, "rolled back")
				f.expect("CLAUDE.md", "chosen")
				f.expect("AGENTS.md", "other")
				f.missing("state/pending.json")
				return
			}
			_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
			if mode == "stale" || mode == "changed-choice" {
				if !errors.Is(err, ErrObservationChanged) {
					t.Fatal(err)
				}
				f.expect("AGENTS.md", "other")
				return
			}
			must(t, err)
			f.expect("AGENTS.md", "chosen")
			if f.plan().HasConflicts() {
				t.Fatal("resolution did not update baseline")
			}
			before = auditTree(t, f.dir)
			f.apply()
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("repeat sync wrote")
			}
		})
	}
}

func TestResolutionAdapters(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) *fixture{"instructions": instructionsFixture, "agent": agentFixture, "hooks": hooksFixture} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.apply()
			old := f.read("claude-source")
			f.write("claude-source", strings.NewReplacer("Shared guidance.", "chosen", "Review carefully.", "chosen", "/usr/bin/true", "/usr/bin/false").Replace(old))
			f.write("codex-source", strings.NewReplacer("Shared guidance.", "other", "Review carefully.", "other", "/usr/bin/true", "/usr/bin/yes").Replace(f.read("codex-source")))
			choices := map[string]string{"portable": "claude"}
			r, err := ReviewResolution(f.path("config.json"), choices)
			must(t, err)
			_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
			must(t, err)
			if f.plan().HasConflicts() {
				t.Fatal("unresolved")
			}
			if name == "instructions" && !strings.Contains(f.read("claude-source"), "Claude only") {
				t.Fatal("lost overlay")
			}
		})
	}
}

func TestResolutionRejectsMissingUnknownAndUnreviewedChoices(t *testing.T) {
	f := newFixture(t)
	f.write("AGENTS.md", "conflict")
	key := f.c.Resources[0].ID
	for _, choices := range []map[string]string{nil, {}, {key: "invalid"}, {"unknown": "claude"}, {key: "shared"}, {key: "claude", "unknown": "codex"}} {
		if _, err := ReviewResolution(f.path("config.json"), choices); err == nil {
			t.Fatal("invalid choices accepted", choices)
		}
	}
	f.missing("state")
	if _, err := Apply(f.c, Options{Resolutions: map[string]string{key: "claude"}}); err == nil {
		t.Fatal("unreviewed resolution accepted")
	}
	f.expect("AGENTS.md", "conflict")
}

func TestResolutionStructuredAndDirectoryItems(t *testing.T) {
	for _, name := range []string{"mcp", "plugin-manifest", "skill", "strict-skill"} {
		t.Run(name, func(t *testing.T) {
			var f *fixture
			var key, source, target, old, selected string
			switch name {
			case "mcp":
				f = mcpFixture(t)
				key, source, target, old, selected = "tools", "claude.json", "codex.toml", "demo-server", "chosen-server"
			case "plugin-manifest":
				f = pluginFixture(t)
				key, source, target, old, selected = "bundle/@manifest", "claude-plugin/.claude-plugin/plugin.json", "codex-plugin/.codex-plugin/plugin.json", "1.0.0", "2.0.0"
			default:
				f = skillFixture(t)
				if name == "strict-skill" {
					f.raw.Resources[0].AllowReformat = true
					f.load()
				}
				key, source, target, old, selected = "demo/SKILL.md", "claude-skill/SKILL.md", "codex-skill/SKILL.md", "portable demo", "selected demo"
			}
			f.apply()
			f.write(source, strings.ReplaceAll(f.read(source), old, selected))
			f.write(target, strings.ReplaceAll(f.read(target), old, "3.0.0"))
			choices := map[string]string{key: "claude"}
			r, err := ReviewResolution(f.path("config.json"), choices)
			must(t, err)
			_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
			must(t, err)
			if !strings.Contains(f.read(target), selected) {
				t.Fatal("selected content not propagated")
			}
			if f.plan().HasConflicts() {
				t.Fatal("unresolved")
			}
		})
	}
}
