package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func interruptedOwnedSync(t *testing.T) (*fixture, syncPending, Journal, *syncOwnership) {
	t.Helper()
	f := newFixture(t)
	func() {
		defer func() {
			if recover() != "fixture-interruption" {
				t.Fatal("expected interruption")
			}
		}()
		_, _ = Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
			if index == 1 {
				panic("fixture-interruption")
			}
			return nil
		}})
	}()
	var pointer syncPending
	raw, err := snapshot(pendingPath(f.c))
	must(t, err)
	must(t, decodeEnrollmentJSON(raw, &pointer))
	if !pointer.CreationOwnership {
		t.Fatal("new pending state lacks receipt requirement")
	}
	var journal Journal
	raw, err = snapshot(f.path("state/backups/" + pointer.Transaction + "/journal.json"))
	must(t, err)
	must(t, decodeEnrollmentJSON(raw, &journal))
	receipt, err := readSyncOwnership(f.c, pointer, journal)
	must(t, err)
	if !receipt.Created[0] {
		t.Fatal("successful creation was not recorded")
	}
	return f, pointer, journal, receipt
}

func TestSyncOwnershipRecovery(t *testing.T) {
	for _, scenario := range []string{"owned", "missing", "unknown-field", "wrong-hash", "wrong-length", "wrong-version", "unrecorded-creation", "stripped-flag", "later-edit"} {
		t.Run(scenario, func(t *testing.T) {
			f, pointer, _, receipt := interruptedOwnedSync(t)
			path := syncOwnershipPath(f.c, pointer.Transaction)
			switch scenario {
			case "missing":
				must(t, os.Remove(path))
			case "unknown-field":
				must(t, os.WriteFile(path, []byte(`{"version":1,"surprise":true}`), 0600))
			case "wrong-hash":
				receipt.Journal = strings.Repeat("0", 64)
				must(t, writeJSON(path, receipt))
			case "wrong-length":
				receipt.Created = []bool{}
				must(t, writeJSON(path, receipt))
			case "wrong-version":
				receipt.Version = 2
				must(t, writeJSON(path, receipt))
			case "unrecorded-creation":
				receipt.Created[0] = false
				must(t, writeJSON(path, receipt))
			case "stripped-flag":
				receipt.Created[0] = false
				must(t, writeJSON(path, receipt))
				must(t, writeJSON(pendingPath(f.c), Pending{pointer.Transaction}))
			case "later-edit":
				f.write("state/shared/rules", "external edit")
			}
			before := auditTree(t, f.dir)
			result, err := Recover(f.c)
			if scenario == "owned" {
				must(t, err)
				if result.Status != "recovered" {
					t.Fatal(result)
				}
				f.missing("state/shared/rules")
				f.missing("state/pending.json")
			} else {
				if err == nil {
					t.Fatal("unsafe recovery accepted")
				}
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("blocked recovery changed data")
				}
			}
		})
	}
}

func TestSyncOwnershipExternalCreationSurvivesRecovery(t *testing.T) {
	for _, same := range []bool{false, true} {
		f := newFixture(t)
		target := f.path("AGENTS.md")
		var external *Snapshot
		_, err := Apply(f.c, Options{BeforeWrite: func(_ int, op Operation) error {
			if op.File == target {
				external = op.After
				if !same {
					external, _ = encoded("external content")
				}
				must(t, writeSnapshot(target, external))
				return errors.New("external creator won")
			}
			return nil
		}})
		contains(t, err, "pending transaction retained")
		_, err = Recover(f.c)
		contains(t, err, "ownership")
		current, err := snapshot(target)
		must(t, err)
		if !equal(current, external) {
			t.Fatal("external creator's file changed")
		}
		// Simulate the operator moving their file aside; recovery can now clean
		// up only the bridge's earlier creation. The external file is preserved.
		must(t, os.Rename(target, f.path("external-saved")))
		_, err = Recover(f.c)
		must(t, err)
		current, err = snapshot(f.path("external-saved"))
		must(t, err)
		if !equal(current, external) {
			t.Fatal("external saved file changed")
		}
		f.missing("state/shared/rules")
	}
}

func TestSyncOwnershipEncodingAndInvalidFlags(t *testing.T) {
	f, pointer, journal, receipt := interruptedOwnedSync(t)
	data, err := json.Marshal(journal)
	must(t, err)
	if strings.Contains(string(data), "creationOwnership") || strings.Contains(string(data), "created") {
		t.Fatal("journal v1 encoding changed")
	}
	legacy, err := json.Marshal(Pending{pointer.Transaction})
	must(t, err)
	if string(legacy) != `{"transaction":"`+pointer.Transaction+`"}` {
		t.Fatal("legacy pending encoding changed")
	}
	journal.Operations[0].Before = journal.Operations[0].After
	receipt, err = newSyncOwnership(journal)
	must(t, err)
	receipt.Created[0] = true
	if err := validateSyncOwnership(journal, receipt); err == nil {
		t.Fatal("owned flag accepted on existing file")
	}
	// Receipts are private and sit beside, not inside, legacy journal snapshots.
	info, err := os.Stat(filepath.Join(f.c.StateDir, "backups", pointer.Transaction, "creation-ownership.json"))
	must(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatal("receipt not private")
	}
}
