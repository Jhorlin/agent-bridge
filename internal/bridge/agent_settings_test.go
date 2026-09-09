package bridge

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func agentSettingsFixture(t *testing.T) *fixture {
	f := agentFixture(t)
	f.raw.Resources[0].PreserveAgentSettings = true
	f.load()
	f.write("claude-source", strings.Replace(f.read("claude-source"), "description: Review code.", "description: Review code.\nmodel: inherit\ntools: Read, Grep\ndisallowedTools: [Bash]\npermissionMode: plan\nmaxTurns: 3", 1))
	return f
}

func TestAgentSettingsRetainedNotTranslated(t *testing.T) {
	f := agentSettingsFixture(t)
	f.apply()
	if strings.Contains(f.read("codex-source"), "model") {
		t.Fatal("Claude model copied to Codex")
	}
	f.write("codex-source", f.read("codex-source")+"\nmodel = 'fixture-model'\nmodel_reasoning_effort = 'low'\nsandbox_mode = 'read-only'\napproval_policy = 'never'\n")
	tree := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(tree, auditTree(t, f.dir)) {
		t.Fatal("local edit caused sync")
	}
	f.write("claude-source", strings.Replace(f.read("claude-source"), "Review carefully.", "New instructions.", 1))
	before := f.read("codex-source")
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("codex-source", before)
	f.apply()
	if !strings.Contains(f.read("codex-source"), "fixture-model") || !strings.Contains(f.read("codex-source"), "read-only") {
		t.Fatal("Codex settings lost")
	}
	f.write("codex-source", strings.Replace(f.read("codex-source"), "New instructions.", "Reverse instructions.", 1))
	f.apply()
	for _, value := range []string{"inherit", "Read, Grep", "Bash", "permissionMode: plan", "maxTurns: 3", "Reverse instructions."} {
		if !strings.Contains(f.read("claude-source"), value) {
			t.Fatal("Claude setting lost", value)
		}
	}
	if strings.Contains(f.read("state/shared/portable"), "model") || strings.Contains(f.read("state/shared/portable"), "permission") {
		t.Fatal("host settings leaked")
	}
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("codex-source", strings.Replace(f.read("codex-source"), "fixture-model", "changed-model", 1))
	_, err = SyncReviewed(f.path("config.json"), r.Observation)
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatal(err)
	}
}

func TestAgentSettingsValidationAndConsent(t *testing.T) {
	for _, field := range []string{"model: true", "tools: [Read, 4]", "permissionMode: invalid", "maxTurns: 0", "hooks: {}", "mcpServers: []", "memory: user", "model: &alias inherit", "tools: !!binary abc", "model: first\nmodel: second"} {
		f := agentSettingsFixture(t)
		f.write("claude-source", "---\nname: reviewer\ndescription: Review.\n"+field+"\n---\nInstructions.\n")
		if !Audit(f.c).Blocked() {
			t.Fatal("bad setting accepted", field)
		}
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("invalid agent wrote")
		}
		f.missing("codex-source")
	}
	f := agentSettingsFixture(t)
	f.raw.Resources[0].PreserveAgentSettings = false
	f.load()
	if !Audit(f.c).Blocked() {
		t.Fatal("local settings accepted without consent")
	}
	f = agentSettingsFixture(t)
	f.apply()
	f.raw.Resources[0].PreserveAgentSettings = false
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("changed identity accepted")
	}
	f = newFixture(t)
	f.raw.Resources[0].PreserveAgentSettings = true
	f.save()
	if _, err := LoadAuditConfig(f.path("config.json")); err == nil {
		t.Fatal("invalid kind accepted")
	}
}

func TestAgentSettingsHistoricalAndConflictSelection(t *testing.T) {
	f := agentSettingsFixture(t)
	tx := initialHistory(t, f)
	f.write("codex-source", f.read("codex-source")+"\nmodel = 'current-model'\n")
	f.write("claude-source", strings.Replace(f.read("claude-source"), "Review carefully.", "Claude edit", 1))
	f.write("codex-source", strings.Replace(f.read("codex-source"), "Review carefully.", "Codex edit", 1))
	choices := map[string]string{"portable": "claude"}
	r, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
	must(t, err)
	choice := HistoryChoice{tx, "portable", "codex", "after"}
	r, err = ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), r.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("codex-source"), "current-model") || !strings.Contains(f.read("codex-source"), "Review carefully.") {
		t.Fatal("history lost local settings")
	}
}

func TestCodexAgentSettingsRejectMalformedValues(t *testing.T) {
	for _, field := range []string{"model = 42", "model_reasoning_effort = 'unknown'", "sandbox_mode = 'invalid'", "approval_policy = { other = true }", "mcp_servers = {}", "hooks = {}"} {
		f := agentSettingsFixture(t)
		f.apply()
		f.write("codex-source", f.read("codex-source")+"\n"+field+"\n")
		if !Audit(f.c).Blocked() {
			t.Fatal("invalid Codex setting accepted", field)
		}
		before := f.read("claude-source")
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("invalid Codex setting wrote")
		}
		f.expect("claude-source", before)
	}
}
