package bridge

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPluginAgentExportServiceAndInheritance(t *testing.T) {
	f := pluginAgentFixture(t)
	child := configInput{Version: 1, StateDir: "child-state", Extends: "../config.json", Resources: []resourceInput{}}
	must(t, writeJSON(f.path("child/profile.json"), child))
	c, err := LoadConfig(f.path("child/profile.json"))
	must(t, err)
	if c.Resources[0].CodexAgentExports["reviewer"] != f.path("codex-home/agents/bridge-bundle-reviewer.toml") {
		t.Fatal("inherited export resolved against wrong profile")
	}
	f.write("bridge-binary", "inert fixture")
	must(t, os.Chmod(f.path("bridge-binary"), 0700))
	launch, err := NewLaunchService(f.path("config.json"), f.path("service-home"), 501)
	must(t, err)
	f.raw.Resources[0].CodexAgentExports["reviewer"] = filepath.Join(launch.Root, "bridge-bundle-reviewer.toml")
	f.load()
	if launch.Install(f.c, f.path("bridge-binary"), false) == nil {
		t.Fatal("LaunchAgent overlapped agent export")
	}
	systemd, err := NewSystemdService(f.path("config.json"), f.path("systemd-home"))
	must(t, err)
	f.raw.Resources[0].CodexAgentExports["reviewer"] = filepath.Join(systemd.Root, "bridge-bundle-reviewer.toml")
	f.load()
	if systemd.Install(f.c, f.path("bridge-binary"), false) == nil {
		t.Fatal("systemd overlapped agent export")
	}
}

func FuzzPluginAgentRoundTrip(f *testing.F) {
	f.Add("---\nname: reviewer\ndescription: Review the fixture.\n---\nReview only.\n")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 32768 {
			return
		}
		item := Item{Resource: Resource{ID: "bundle"}, Relative: "agents/reviewer.md"}
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		common, err := normalizePluginAgent(item, "claude", raw)
		if err != nil {
			return
		}
		for _, side := range sides {
			rendered, err := renderPluginAgent(item, side, common, nil)
			must(t, err)
			again, err := normalizePluginAgent(item, side, rendered)
			must(t, err)
			if !equal(common, again) {
				t.Fatal("export round-trip changed portable fields", side)
			}
		}
	})
}

func pluginAgentFixture(t *testing.T) *fixture {
	f := pluginFixture(t)
	f.raw.Resources[0].AllowReformat = true
	f.raw.Resources[0].CodexAgentExports = map[string]string{"reviewer": "codex-home/agents/bridge-bundle-reviewer.toml"}
	f.load()
	f.write("claude-plugin/agents/reviewer.md", "---\nname: reviewer\ndescription: BRIDGE_PLUGIN_EXPORT_DESCRIPTION\n---\nReview the fixture only.\n")
	return f
}

func TestPluginAgentExportRoundTrip(t *testing.T) {
	f := pluginAgentFixture(t)
	f.apply()
	if !strings.Contains(f.read("codex-home/agents/bridge-bundle-reviewer.toml"), "bridge-bundle-reviewer") {
		t.Fatal("export not namespaced")
	}
	f.missing("codex-plugin/agents/reviewer.md")
	f.write("codex-home/agents/bridge-bundle-reviewer.toml", strings.ReplaceAll(f.read("codex-home/agents/bridge-bundle-reviewer.toml"), "Review the fixture only.", "Reverse instructions."))
	f.apply()
	if !strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "Reverse instructions.") || strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "bridge-bundle-reviewer") {
		t.Fatal("reverse export was not mapped")
	}
	before := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("repeat export changed files")
	}
	flat, err := flatProfile(f.c)
	must(t, err)
	must(t, writeSnapshot(f.path("flat.json"), flat))
	c, err := LoadConfig(f.path("flat.json"))
	must(t, err)
	if !reflect.DeepEqual(c.Resources, f.c.Resources) {
		t.Fatal("enrollment lost export identity")
	}
}

func TestPluginAgentExportSettingsHistory(t *testing.T) {
	f := pluginAgentFixture(t)
	f.raw.Resources[0].PreserveAgentSettings = true
	f.load()
	f.write("claude-plugin/agents/reviewer.md", strings.Replace(f.read("claude-plugin/agents/reviewer.md"), "name: reviewer\n", "name: reviewer\nmodel: opus\n", 1))
	tx := initialHistory(t, f)
	f.write("claude-plugin/agents/reviewer.md", strings.ReplaceAll(strings.ReplaceAll(f.read("claude-plugin/agents/reviewer.md"), "model: opus", "model: sonnet"), "Review the fixture only.", "New instructions."))
	f.apply()
	f.write("codex-home/agents/bridge-bundle-reviewer.toml", f.read("codex-home/agents/bridge-bundle-reviewer.toml")+"sandbox_mode='read-only'\nmodel_reasoning_effort='low'\n")
	choice := HistoryChoice{tx, "bundle/agents/reviewer.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "model: sonnet") || !strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "Review the fixture only.") {
		t.Fatal("history lost current Claude settings or old body")
	}
	s, err := snapshot(f.path("codex-home/agents/bridge-bundle-reviewer.toml"))
	must(t, err)
	doc, err := document("codex", s)
	must(t, err)
	if doc["sandbox_mode"] != "read-only" || doc["model_reasoning_effort"] != "low" {
		t.Fatal("history lost current Codex settings")
	}
	if _, exists := doc["model"]; exists {
		t.Fatal("history copied Claude model")
	}
}

func TestPluginAgentExportHistoryConflictAndReview(t *testing.T) {
	f := pluginAgentFixture(t)
	tx := initialHistory(t, f)
	original := f.read("codex-home/agents/bridge-bundle-reviewer.toml")
	f.write("codex-home/agents/bridge-bundle-reviewer.toml", strings.ReplaceAll(original, "Review the fixture only.", "Codex edit."))
	f.write("claude-plugin/agents/reviewer.md", strings.ReplaceAll(f.read("claude-plugin/agents/reviewer.md"), "Review the fixture only.", "Claude edit."))
	if !f.plan().HasConflicts() {
		t.Fatal("export conflict lost")
	}
	choices := map[string]string{"bundle/agents/reviewer.md": "codex"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	choice := HistoryChoice{tx, "bundle/agents/reviewer.md", "codex", "after"}
	h, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), h.Observation, choice)
	must(t, err)
	f.expect("codex-home/agents/bridge-bundle-reviewer.toml", original)
	review, err = ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("codex-home/agents/bridge-bundle-reviewer.toml", original+"# later formatting\n")
	_, err = SyncReviewed(f.path("config.json"), review.Observation)
	contains(t, err, "changed")
}

func TestPluginAgentExportRollbackRecovery(t *testing.T) {
	for fail := 0; fail < 3; fail++ {
		f := pluginAgentFixture(t)
		f.apply()
		old := f.read("codex-home/agents/bridge-bundle-reviewer.toml")
		f.write("claude-plugin/agents/reviewer.md", strings.ReplaceAll(f.read("claude-plugin/agents/reviewer.md"), "Review the fixture only.", "Updated instructions."))
		_, err := Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
			if index == fail {
				return errors.New("injected")
			}
			return nil
		}})
		contains(t, err, "rolled back")
		f.expect("codex-home/agents/bridge-bundle-reviewer.toml", old)
		f.missing("state/pending.json")
		f.apply()
	}
	f := pluginAgentFixture(t)
	f.apply()
	f.write("claude-plugin/agents/reviewer.md", strings.ReplaceAll(f.read("claude-plugin/agents/reviewer.md"), "Review the fixture only.", "Updated instructions."))
	_, err := Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
		if index == 2 {
			f.write("codex-home/agents/bridge-bundle-reviewer.toml", "later edit")
			return errors.New("injected")
		}
		return nil
	}})
	contains(t, err, "pending transaction retained")
	_, err = Recover(f.c)
	contains(t, err, "later edit")
	f.expect("codex-home/agents/bridge-bundle-reviewer.toml", "later edit")
}

func TestPluginAgentExportRefusals(t *testing.T) {
	for _, mode := range []string{"unlisted", "bundled-codex", "wrong-name", "policy", "root-macro", "deleted", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := pluginAgentFixture(t)
			f.apply()
			switch mode {
			case "unlisted":
				f.write("claude-plugin/agents/other.md", f.read("claude-plugin/agents/reviewer.md"))
			case "bundled-codex":
				f.write("codex-plugin/agents/reviewer.md", f.read("claude-plugin/agents/reviewer.md"))
			case "wrong-name":
				f.write("codex-home/agents/bridge-bundle-reviewer.toml", strings.ReplaceAll(f.read("codex-home/agents/bridge-bundle-reviewer.toml"), "bridge-bundle-reviewer", "explorer"))
			case "policy":
				f.write("codex-home/agents/bridge-bundle-reviewer.toml", f.read("codex-home/agents/bridge-bundle-reviewer.toml")+"sandbox_mode='danger-full-access'\n")
			case "root-macro":
				f.write("claude-plugin/agents/reviewer.md", f.read("claude-plugin/agents/reviewer.md")+"Read ${CLAUDE_PLUGIN_ROOT}/private.md.\n")
			case "deleted":
				must(t, os.Remove(f.path("codex-home/agents/bridge-bundle-reviewer.toml")))
			case "symlink":
				must(t, os.Rename(f.path("codex-home/agents/bridge-bundle-reviewer.toml"), f.path("external-agent")))
				must(t, os.Symlink(f.path("external-agent"), f.path("codex-home/agents/bridge-bundle-reviewer.toml")))
			}
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("unsafe export accepted")
			}
			f.missing("state/pending.json")
		})
	}
}

func TestPluginAgentExportOwnershipAndIdentity(t *testing.T) {
	f := pluginAgentFixture(t)
	other := configInput{Version: 1, StateDir: "other-state", Resources: []resourceInput{{ID: "other", Kind: "portable-file", Scope: "global", Claude: "codex-home/agents/bridge-bundle-reviewer.toml", Codex: "other-target"}}}
	must(t, writeJSON(f.path("other.json"), other))
	report, err := CheckOverlaps([]string{f.path("config.json"), f.path("other.json")})
	must(t, err)
	if len(report.Overlaps) == 0 {
		t.Fatal("export missing from ownership claims")
	}
	f.apply()
	f.raw.Resources[0].CodexAgentExports["reviewer"] = "elsewhere/bridge-bundle-reviewer.toml"
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("export identity change accepted")
	}
	for _, dest := range []string{"codex-plugin/bridge-bundle-reviewer.toml", "state/bridge-bundle-reviewer.toml", "codex-home/agents/reviewer.toml", "~/agents/bridge-bundle-reviewer.toml"} {
		f := pluginAgentFixture(t)
		f.raw.Resources[0].CodexAgentExports["reviewer"] = dest
		f.save()
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatal("unsafe export config accepted", dest)
		}
	}
}
