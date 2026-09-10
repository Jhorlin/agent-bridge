package bridge

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

// Corrupt durable state must retain both native files and recovery evidence.
// Removing any pointer/journal validation must make its corresponding case fail.
func TestRecoveryRejectsCorruptEvidenceWithoutWrites(t *testing.T) {
	const tx = "11111111-1111-4111-8111-111111111111"
	for _, kind := range []string{"pointer-json", "pointer-traversal", "missing-journal", "journal-json", "journal-version", "journal-null-operations", "journal-directory", "journal-link"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.apply()
			f.write("state/pending.json", `{"transaction":"`+tx+`"}`)
			journal := "state/backups/" + tx + "/journal.json"
			f.write(journal, `{"version":1,"operations":[]}`)
			switch kind {
			case "pointer-json":
				f.write("state/pending.json", `{"transaction":`)
			case "pointer-traversal":
				f.write("state/pending.json", `{"transaction":"../../CANARY_PRIVATE"}`)
			case "missing-journal":
				must(t, os.Remove(f.path(journal)))
			case "journal-json":
				f.write(journal, `{"version":`)
			case "journal-version":
				f.write(journal, `{"version":2,"operations":[]}`)
			case "journal-null-operations":
				f.write(journal, `{"version":1,"operations":null}`)
			case "journal-directory":
				must(t, os.Remove(f.path(journal)))
				must(t, os.Mkdir(f.path(journal), 0700))
			case "journal-link":
				must(t, os.Rename(f.path(journal), f.path("CANARY_PRIVATE")))
				must(t, os.Symlink(f.path("CANARY_PRIVATE"), f.path(journal)))
			}
			before := auditTree(t, f.dir)
			var events []diagnostics.Event
			_, err := RecoverObserved(f.c, func(e diagnostics.Event) { events = append(events, e) })
			if err == nil {
				t.Fatal("corrupt recovery evidence accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("failed recovery changed files or evidence")
			}
			if len(events) != 1 || events[0].Code == "ok" || events[0].Stage != "recover" {
				t.Fatal("recovery failure not observable", events)
			}
			data, err := json.Marshal(events)
			must(t, err)
			if strings.Contains(string(data), "CANARY_PRIVATE") || strings.Contains(string(data), f.dir) {
				t.Fatal("recovery diagnostic leaked private inputs")
			}
		})
	}
}

// A malformed later operation must not partially roll back an earlier one.
func TestRecoveryPrevalidatesAllAfterSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after *Snapshot
		valid bool
	}{
		{"missing", nil, false},
		{"invalid-data", &Snapshot{Data: "not base64", Mode: 0600}, false},
		{"invalid-mode", &Snapshot{Data: "b25l", Mode: 01000}, false},
		{"valid-control", &Snapshot{Data: "b25l", Mode: 0600}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.apply()
			// Match the journal explicitly so an unrelated later-edit mismatch
			// cannot disguise missing after-snapshot validation.
			must(t, os.Chmod(f.path("CLAUDE.md"), 0600))
			must(t, os.Chmod(f.path("AGENTS.md"), 0600))
			const tx = "11111111-1111-4111-8111-111111111111"
			must(t, writeJSON(pendingPath(f.c), Pending{tx}))
			must(t, writeJSON(f.path("state/backups/"+tx+"/journal.json"), Journal{Version: 1, Operations: []Operation{
				{Label: "validated-first", File: f.path("CLAUDE.md"), Before: &Snapshot{Data: "b25l", Mode: 0600}, After: tc.after},
				{Label: "would-remove", File: f.path("AGENTS.md"), Before: nil, After: &Snapshot{Data: "b25l", Mode: 0600}},
			}}))
			before := auditTree(t, f.dir)
			_, err := Recover(f.c)
			if tc.valid {
				must(t, err)
				f.expect("CLAUDE.md", "one")
				f.missing("AGENTS.md")
				f.missing("state/pending.json")
				return
			}
			if err == nil {
				t.Fatal("invalid after snapshot accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("recovery partially modified files")
			}
		})
	}
}
