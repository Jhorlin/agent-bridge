package bridge

import (
	"errors"
	"reflect"
	"testing"
)

func TestRetirementReviewBindsRoster(t *testing.T) {
	for _, dependent := range []bool{false, true} {
		f := rosterFixture(t)
		f.apply()
		review, err := ReviewRetirement(f.path("config.json"), "rules")
		must(t, err)
		child := configInput{Version: 1, StateDir: "child-state", CoordinationDir: "coordination", Resources: []resourceInput{}}
		if dependent {
			child.Extends = "config.json"
			child.Disable = []string{"rules"}
		}
		must(t, writeJSON(f.path("child.json"), child))
		writeRoster(t, f, []string{f.path("config.json"), f.path("child.json")})
		before := auditTree(t, f.dir)
		_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
		if dependent {
			contains(t, err, "inherits")
		} else if !errors.Is(err, ErrObservationChanged) {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("stale roster changed files")
		}
	}
}

func TestRetiredSupportingHistoryIsReadOnly(t *testing.T) {
	f, change := supportingFixture(t)
	review, err := ReviewFileChange(f.path("config.json"), change)
	must(t, err)
	result, err := ApplyFileChange(f.path("config.json"), review.Observation, change)
	must(t, err)
	retirement, err := ReviewRetirement(f.path("config.json"), "demo")
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), retirement.Observation, "demo")
	must(t, err)
	before := auditTree(t, f.dir)
	entries, err := FileChangeHistory(f.path("config.json"))
	must(t, err)
	if len(entries) != 1 || entries[0].Transaction != result.Transaction {
		t.Fatal(entries)
	}
	_, err = ReviewFileChangeUndo(f.path("config.json"), result.Transaction)
	if err == nil {
		t.Fatal("retired supporting file remained writable")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("retired history inspection changed files")
	}
}

func TestRetiredHistoryRejectsUnregisteredJournalTarget(t *testing.T) {
	f := newFixture(t)
	tx := initialHistory(t, f)
	review, err := ReviewRetirement(f.path("config.json"), "rules")
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
	must(t, err)
	c, err := LoadConfig(f.path("config.json"))
	must(t, err)
	j, _, err := readHistoryJournal(c, tx)
	must(t, err)
	j.Operations[0].File = f.path("never-registered")
	must(t, writeJSON(f.path("state/backups/"+tx+"/journal.json"), j))
	before := auditTree(t, f.dir)
	_, err = History(f.path("config.json"))
	if err == nil {
		t.Fatal("archive accepted an arbitrary target")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("invalid journal changed files")
	}
}

func TestRetiredJournalDoesNotAuthorizePendingRecovery(t *testing.T) {
	f := newFixture(t)
	tx := initialHistory(t, f)
	review, err := ReviewRetirement(f.path("config.json"), "rules")
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
	must(t, err)
	c, err := LoadConfig(f.path("config.json"))
	must(t, err)
	must(t, writeJSON(pendingPath(c), Pending{tx}))
	before := auditTree(t, f.dir)
	_, err = Recover(c)
	contains(t, err, "invalid target")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("archived identity expanded recovery authority")
	}
}

func TestMCPAmbiguousHeadersBlockWholeSync(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", `{"mcpServers":{"docs":{"type":"http","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer ${FIRST_FIXTURE_TOKEN}","authorization":"Bearer ${SECOND_FIXTURE_TOKEN}"}}}}`)
	before := auditTree(t, f.dir)
	_, err := Plan(f.c)
	contains(t, err, "duplicate case-insensitive")
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("invalid MCP plan wrote files")
	}
	_, err = Apply(f.c, Options{})
	contains(t, err, "duplicate case-insensitive")
	f.missing("codex.toml")
	f.missing("state/pending.json")
	f.missing("state/manifest.json")
}
