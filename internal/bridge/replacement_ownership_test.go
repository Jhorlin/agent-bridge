package bridge

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func interruptedReplacement(t *testing.T) (*fixture, syncPending, Journal, *syncOwnership) {
	t.Helper()
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "new version")
	func() {
		defer func() {
			if recover() != "replacement-interruption" {
				t.Fatal("expected interruption")
			}
		}()
		_, _ = Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
			if index == 1 {
				panic("replacement-interruption")
			}
			return nil
		}})
	}()
	var pointer syncPending
	raw, err := snapshot(pendingPath(f.c))
	must(t, err)
	must(t, decodeEnrollmentJSON(raw, &pointer))
	if !pointer.WriteOwnership {
		t.Fatal("new pending pointer lacks replacement evidence requirement")
	}
	var journal Journal
	raw, err = snapshot(f.path("state/backups/" + pointer.Transaction + "/journal.json"))
	must(t, err)
	must(t, decodeEnrollmentJSON(raw, &journal))
	receipt, err := readSyncOwnership(f.c, pointer, journal)
	must(t, err)
	if journal.Operations[0].Before == nil || !receipt.Written[0] || receipt.Written[1] {
		t.Fatal("incorrect replacement progress")
	}
	return f, pointer, journal, receipt
}

func TestReplacementOwnershipRecoveryBoundaries(t *testing.T) {
	for _, scenario := range []string{"owned", "receipt-gap", "missing-written", "short-written", "legacy-v1", "downgraded-receipt", "external-after", "external-different"} {
		t.Run(scenario, func(t *testing.T) {
			f, pointer, journal, receipt := interruptedReplacement(t)
			path := syncOwnershipPath(f.c, pointer.Transaction)
			switch scenario {
			case "receipt-gap":
				receipt.Written[0] = false
				must(t, writeJSON(path, receipt))
			case "missing-written":
				receipt.Written = nil
				must(t, writeJSON(path, receipt))
			case "short-written":
				receipt.Written = []bool{true}
				must(t, writeJSON(path, receipt))
			case "legacy-v1", "downgraded-receipt":
				receipt.Version = 1
				receipt.Written = nil
				must(t, writeJSON(path, receipt))
				if scenario == "legacy-v1" {
					pointer.WriteOwnership = false
					must(t, writeJSON(pendingPath(f.c), pointer))
				}
			case "external-after":
				must(t, writeSnapshot(journal.Operations[1].File, journal.Operations[1].After))
			case "external-different":
				f.write("AGENTS.md", "independent external edit")
			}
			before := auditTree(t, f.dir)
			result, err := Recover(f.c)
			if scenario == "owned" {
				must(t, err)
				if result.Status != "recovered" {
					t.Fatal(result)
				}
				f.expect("state/shared/rules", "one")
				f.expect("AGENTS.md", "one")
				f.expect("CLAUDE.md", "new version")
			} else {
				if err == nil {
					t.Fatal("ambiguous replacement recovered")
				}
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("blocked recovery changed files")
				}
			}
		})
	}
}

func TestReplacementOwnershipRecoveryAfterExternalEditPreserved(t *testing.T) {
	f, _, journal, _ := interruptedReplacement(t)
	op := journal.Operations[1]
	must(t, writeSnapshot(op.File, op.After))
	_, err := Recover(f.c)
	contains(t, err, "replacement ownership")
	// Operator preserves the external save, then restores the recorded before
	// snapshot. No automatic action or inferred consent discards the external edit.
	must(t, os.Rename(op.File, f.path("external-saved")))
	must(t, writeSnapshot(op.File, op.Before))
	_, err = Recover(f.c)
	must(t, err)
	saved, err := snapshot(f.path("external-saved"))
	must(t, err)
	if !equal(saved, op.After) {
		t.Fatal("external saved version lost")
	}
	f.expect("state/shared/rules", "one")
	f.expect("AGENTS.md", "one")
}

func TestReplacementOwnershipProcessInterruption(t *testing.T) {
	f := newFixture(t)
	f.apply()
	f.write("CLAUDE.md", "new version")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), "AGENT_BRIDGE_CRASH_CONFIG="+f.path("config.json"))
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 91 {
		t.Fatalf("unexpected helper exit: %v", err)
	}
	_, err = Recover(f.c)
	contains(t, err, "stale sync.lock")
	must(t, os.Remove(f.path("state/sync.lock")))
	_, err = Recover(f.c)
	must(t, err)
	f.expect("state/shared/rules", "one")
	f.expect("AGENTS.md", "one")
	f.expect("CLAUDE.md", "new version")
	f.apply()
	f.expect("AGENTS.md", "new version")
}

func TestVersionOneCreationReceiptStillRecovers(t *testing.T) {
	f, pointer, _, receipt := interruptedOwnedSync(t)
	receipt.Version = 1
	receipt.Written = nil
	pointer.WriteOwnership = false
	must(t, writeJSON(syncOwnershipPath(f.c, pointer.Transaction), receipt))
	must(t, writeJSON(pendingPath(f.c), pointer))
	_, err := Recover(f.c)
	must(t, err)
	f.missing("state/shared/rules")
	f.missing("state/pending.json")
}
