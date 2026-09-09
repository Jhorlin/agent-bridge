package bridge

import (
	"os"
	"reflect"
	"testing"
)

// Reproductions from the holistic review, retained as safety regressions.
func TestHolisticReviewRollbackPreservesExternalCreation(t *testing.T) {
	f := newFixture(t)
	target := f.path("AGENTS.md")
	var created *Snapshot
	_, err := Apply(f.c, Options{BeforeWrite: func(_ int, op Operation) error {
		if op.File == target {
			created = op.After
			return writeSnapshot(target, created) // Another writer creates the same bytes.
		}
		return nil
	}})
	if err == nil || created == nil {
		t.Fatal("probe did not reach concurrent-creation refusal")
	}
	current, err := snapshot(target)
	must(t, err)
	if !equal(current, created) {
		t.Fatal("rollback deleted a file created by another writer")
	}
}

func TestHolisticReviewRetirementPreservesUnrelatedHistory(t *testing.T) {
	f := newFixture(t)
	f.raw.Resources = append(f.raw.Resources, resourceInput{ID: "other", Kind: "portable-file", Scope: "project", Claude: "other-claude", Codex: "other-codex"})
	f.write("other-claude", "unrelated instructions")
	f.load()
	f.apply()
	entries, err := History(f.path("config.json"))
	must(t, err)
	if len(entries) != 1 {
		t.Fatal("expected one mixed-resource journal")
	}
	review, err := ReviewRetirement(f.path("config.json"), "rules")
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
	must(t, err)
	_, err = History(f.path("config.json"))
	if err != nil {
		t.Errorf("retiring rules blocked history for active other resource: %v", err)
	}
	_, err = ReviewHistory(f.path("config.json"), HistoryChoice{Transaction: entries[0].Transaction, Key: "other", Side: "codex", Snapshot: "after"})
	if err != nil {
		t.Errorf("retirement also blocked restoring active other resource: %v", err)
	}
	current, err := LoadConfig(f.path("config.json"))
	must(t, err)
	f.write("other-claude", "new unrelated version")
	_, err = Apply(current, Options{})
	must(t, err)
	choice := HistoryChoice{Transaction: entries[0].Transaction, Key: "other", Side: "codex", Snapshot: "after"}
	checkpoint, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), checkpoint.Observation, choice)
	must(t, err)
	f.expect("other-claude", "unrelated instructions")
	f.expect("other-codex", "unrelated instructions")
	f.expect("CLAUDE.md", "one")
	f.expect("AGENTS.md", "one")
	choice.Key = "rules"
	_, err = ReviewHistory(f.path("config.json"), choice)
	contains(t, err, "not registered")
}

func TestHolisticReviewRetirementKeepsEnrolledChildValid(t *testing.T) {
	f := rosterFixture(t)
	child := configInput{Version: 1, StateDir: "child-state", Extends: "config.json", Disable: []string{"rules"}, Resources: []resourceInput{}}
	must(t, writeJSON(f.path("child.json"), child))
	writeRoster(t, f, []string{f.path("config.json"), f.path("child.json")})
	f.apply()
	before := auditTree(t, f.dir)
	review, err := ReviewRetirement(f.path("config.json"), "rules")
	contains(t, err, "inherits")
	_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
	contains(t, err, "inherits")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("refusal changed enrolled profile state")
	}
	if _, err := LoadConfig(f.path("child.json")); err != nil {
		t.Errorf("retirement committed a change that invalidates an enrolled child: %v", err)
	}
	current, err := LoadConfig(f.path("config.json"))
	must(t, err)
	_, err = Apply(current, Options{})
	if err != nil {
		t.Errorf("retirement also blocked the parent through ownership validation: %v", err)
	}
	if _, err := os.Stat(f.path("AGENTS.md")); err != nil {
		t.Fatal(err)
	}
}

func TestHolisticReviewRejectsAmbiguousBearerHeaders(t *testing.T) {
	_, err := serverFromNative("claude", map[string]any{
		"type": "http", "url": "https://example.invalid/mcp",
		"headers": map[string]any{
			"Authorization": "Bearer ${FIRST_FIXTURE_TOKEN}",
			"authorization": "Bearer ${SECOND_FIXTURE_TOKEN}",
		},
	})
	if err == nil {
		t.Fatal("accepted competing case-insensitive bearer headers and silently discarded one token reference")
	}
}
