package bridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Exercise the public configuration boundary before the opt-in is implemented.
func TestPluginCommandLocalSettingsRoundTrip(t *testing.T) {
	f := pluginFixture(t)
	var config map[string]any
	must(t, json.Unmarshal([]byte(f.read("config.json")), &config))
	config["resources"].([]any)[0].(map[string]any)["preserveCommandSettings"] = true
	config["resources"].([]any)[0].(map[string]any)["allowReformat"] = true
	data, err := json.Marshal(config)
	must(t, err)
	f.write("config.json", string(data))
	f.c, err = LoadConfig(f.path("config.json"))
	must(t, err)
	local := "---\ndescription: A portable command.\nmodel: sonnet\nallowed-tools: [Read, Grep]\nargument-hint: '[topic]'\n---\nReturn the fixed word fixture.\n"
	f.write("claude-plugin/commands/demo.md", local)
	f.apply()
	f.expect("claude-plugin/commands/demo.md", local)
	for _, path := range []string{"codex-plugin/commands/demo.md", "state/shared/demo/commands/demo.md"} {
		// The fixture resource ID determines the shared path.
		if strings.HasPrefix(path, "state/") {
			path = "state/shared/" + f.c.Resources[0].ID + "/commands/demo.md"
		}
		if raw := f.read(path); strings.Contains(raw, "sonnet") || strings.Contains(raw, "allowed-tools") || strings.Contains(raw, "argument-hint") {
			t.Fatal("Claude-local command choices leaked to portable output")
		}
	}
	f.write("codex-plugin/commands/demo.md", strings.Replace(f.read("codex-plugin/commands/demo.md"), "fixed word", "updated word", 1))
	f.apply()
	if raw := f.read("claude-plugin/commands/demo.md"); !strings.Contains(raw, "updated word") || !strings.Contains(raw, "model: sonnet") || !strings.Contains(raw, "allowed-tools:") {
		t.Fatal("reverse sync lost portable edit or local command settings")
	}
	for _, item := range f.plan().Summaries() {
		if item.Status != "in-sync" {
			t.Fatal("command overlay did not converge")
		}
	}
}

func commandSettingsFixture(t *testing.T) *fixture {
	f := pluginFixture(t)
	f.raw.Resources[0].PreserveCommandSettings = true
	f.raw.Resources[0].AllowReformat = true
	f.load()
	f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "description:", "model: sonnet\nallowed-tools: Read\nargument-hint: '[topic]'\ndescription:", 1))
	return f
}

func TestPluginCommandSettingsRecoveryHistoryAndStaleReview(t *testing.T) {
	f := commandSettingsFixture(t)
	tx := initialHistory(t, f)
	original := f.read("claude-plugin/commands/demo.md")
	f.write("codex-plugin/commands/demo.md", strings.Replace(f.read("codex-plugin/commands/demo.md"), "fixed word", "updated word", 1))
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("claude-plugin/commands/demo.md", original)
	f.apply()
	choice := HistoryChoice{tx, f.c.Resources[0].ID + "/commands/demo.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	f.write("claude-plugin/commands/demo.md", strings.Replace(f.read("claude-plugin/commands/demo.md"), "model: sonnet", "model: haiku", 1))
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	contains(t, err, "changed")
	review, err = ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	if raw := f.read("claude-plugin/commands/demo.md"); !strings.Contains(raw, "fixed word") || !strings.Contains(raw, "model: haiku") {
		t.Fatal("history lost current local settings or historical portable text")
	}
	plan := f.plan()
	f.write("claude-plugin/commands/demo.md", strings.Replace(f.read("claude-plugin/commands/demo.md"), "model: haiku", "model: sonnet", 1))
	_, err = Apply(f.c, Options{ExpectedObservation: Observation(f.c, plan)})
	contains(t, err, "changed")
	for _, item := range f.plan().Items {
		if len(item.Writes) != 0 {
			t.Fatal("local-only setting edit caused portable writes")
		}
	}
}

func TestPluginCommandSettingsConflictKeepsLocalChoices(t *testing.T) {
	f := commandSettingsFixture(t)
	f.apply()
	for _, side := range []string{"claude", "codex"} {
		path := side + "-plugin/commands/demo.md"
		f.write(path, strings.Replace(f.read(path), "fixed word", side+" choice", 1))
	}
	if !f.plan().HasConflicts() {
		t.Fatal("command conflict lost")
	}
	choices := map[string]string{f.c.Resources[0].ID + "/commands/demo.md": "codex"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), review.Observation, choices)
	must(t, err)
	if raw := f.read("claude-plugin/commands/demo.md"); !strings.Contains(raw, "codex choice") || !strings.Contains(raw, "model: sonnet") {
		t.Fatal("conflict choice lost local settings")
	}
}

func TestPluginCommandSettingsRejectUnsafeAndForeignMetadata(t *testing.T) {
	for _, side := range []string{"claude", "codex", "shared"} {
		for _, field := range []string{"model: [sonnet]", "allowed-tools: [12]", "argument-hint: true", "hooks: {}", "disable-model-invocation: true", "context: fork", "model: \"bad\\nmodel\""} {
			t.Run(side+"/"+field, func(t *testing.T) {
				f := commandSettingsFixture(t)
				f.apply()
				path := side + "-plugin/commands/demo.md"
				if side == "shared" {
					path = "state/shared/" + f.c.Resources[0].ID + "/commands/demo.md"
				}
				f.write(path, strings.Replace(commandFixture, "description:", field+"\ndescription:", 1))
				before := auditTree(t, f.dir)
				if _, err := Apply(f.c, Options{}); err == nil {
					t.Fatal("unsupported command policy accepted")
				}
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("rejected command changed files")
				}
			})
		}
	}
	for _, side := range []string{"codex", "shared"} {
		f := commandSettingsFixture(t)
		f.apply()
		path := side + "-plugin/commands/demo.md"
		if side == "shared" {
			path = "state/shared/" + f.c.Resources[0].ID + "/commands/demo.md"
		}
		f.write(path, strings.Replace(commandFixture, "description:", "model: sonnet\ndescription:", 1))
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("Claude-local setting accepted in foreign canonical input")
		}
	}
}

func TestPluginCommandSettingsEnrollmentAndConfiguration(t *testing.T) {
	f := commandSettingsFixture(t)
	flat, err := flatProfile(f.c)
	must(t, err)
	data, err := snapshotBytes(flat)
	must(t, err)
	f.write("flat.json", string(data))
	c, err := LoadConfig(f.path("flat.json"))
	must(t, err)
	if !reflect.DeepEqual(c.Resources, f.c.Resources) {
		t.Fatal("flattening lost command policy")
	}
	f.apply()
	f.raw.Resources[0].PreserveCommandSettings = false
	f.raw.Resources[0].AllowReformat = false
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("explicit resource identity downgrade accepted")
	}
}

func TestConventionPluginCommandSettingsUpgrade(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json", `{"name":"demo"}`)
	f.write(".agent-bridge-plugins/claude/demo/commands/demo.md", commandFixture)
	reloadConventions(t, f)
	f.apply()
	// Model an existing pre-option baseline: the portable projection is the
	// same, and only the newly added optional resource flag is absent.
	var manifest Manifest
	must(t, json.Unmarshal([]byte(f.read("state/manifest.json")), &manifest))
	for id, r := range manifest.Resources {
		r.PreserveCommandSettings = false
		manifest.Resources[id] = r
	}
	data, err := json.Marshal(manifest)
	must(t, err)
	f.write("state/manifest.json", string(data))
	f.write(".agent-bridge-plugins/claude/demo/commands/demo.md", strings.Replace(commandFixture, "description:", "model: sonnet\ndescription:", 1))
	reloadConventions(t, f)
	f.apply()
	reloadConventions(t, f)
	f.apply()
	if !f.c.Resources[0].PreserveCommandSettings {
		t.Fatal("convention command overlay was not enabled")
	}
	if strings.Contains(f.read(".agent-bridge-plugins/codex/demo/commands/demo.md"), "model:") {
		t.Fatal("convention upgrade exported a Claude model")
	}
}

func TestConventionPluginCommandSettingsPreUpgradeHistory(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json", `{"name":"demo"}`)
	f.write(".agent-bridge-plugins/claude/demo/commands/demo.md", commandFixture)
	reloadConventions(t, f)
	tx := initialHistory(t, f)
	// Reconstruct the pre-option journal schema: only the additive flag is
	// omitted. Snapshot data, resource paths and portable digests stay intact.
	journalPath := "state/backups/" + tx + "/journal.json"
	var journal Journal
	must(t, json.Unmarshal([]byte(f.read(journalPath)), &journal))
	for i := range journal.Operations {
		op := &journal.Operations[i]
		if op.File == manifestPath(f.c) {
			var m Manifest
			must(t, decode(op.After, &m))
			for id, r := range m.Resources {
				r.PreserveCommandSettings = false
				m.Resources[id] = r
			}
			var err error
			op.After, err = encoded(m)
			must(t, err)
		}
	}
	data, err := json.Marshal(journal)
	must(t, err)
	f.write(journalPath, string(data))
	f.write(".agent-bridge-plugins/claude/demo/commands/demo.md", strings.Replace(commandFixture, "description:", "model: haiku\ndescription:", 1))
	f.write(".agent-bridge-plugins/codex/demo/commands/demo.md", strings.Replace(commandFixture, "fixed word", "updated word", 1))
	f.apply()
	choice := HistoryChoice{tx, f.c.Resources[0].ID + "/commands/demo.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	if raw := f.read(".agent-bridge-plugins/claude/demo/commands/demo.md"); !strings.Contains(raw, "fixed word") || !strings.Contains(raw, "model: haiku") {
		t.Fatal("pre-upgrade history lost current overlay or historical body")
	}
}
