package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func initialHistory(t *testing.T, f *fixture) string {
	t.Helper()
	f.apply()
	h, err := History(f.path("config.json"))
	must(t, err)
	if len(h) != 1 {
		t.Fatal(h)
	}
	return h[0].Transaction
}

func TestHistoricalRestoreAndFreshness(t *testing.T) {
	for _, mode := range []string{"restore", "stale-input", "stale-journal", "rollback", "later-edit", "identity", "absent", "traversal", "unknown-fields", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			tx := initialHistory(t, f)
			f.write("CLAUDE.md", "new version")
			f.apply()
			choice := HistoryChoice{tx, "rules", "codex", "after"}
			before := auditTree(t, f.dir)
			r, err := ReviewHistory(f.path("config.json"), choice)
			must(t, err)
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("history review wrote")
			}
			journal := filepath.Join("state", "backups", tx, "journal.json")
			switch mode {
			case "stale-input":
				f.write("CLAUDE.md", "later")
			case "stale-journal":
				f.write(journal, f.read(journal)+"\n")
			case "identity":
				j, _, err := readHistoryJournal(f.c, tx)
				must(t, err)
				for n, op := range j.Operations {
					if op.Label == "manifest" {
						var m Manifest
						must(t, decode(op.After, &m))
						delete(m.Resources, "rules")
						j.Operations[n].After, err = encoded(m)
						must(t, err)
					}
				}
				must(t, writeJSON(f.path(journal), j))
			case "absent":
				choice.Snapshot = "before"
			case "traversal":
				choice.Transaction = "../elsewhere"
			case "unknown-fields":
				f.write(journal, strings.Replace(f.read(journal), `"version": 1`, `"version": 1, "unknown": true`, 1))
			case "symlink":
				must(t, os.Rename(f.path(journal), f.path("saved-journal")))
				must(t, os.Symlink(f.path("saved-journal"), f.path(journal)))
			case "rollback", "later-edit":
				_, err = Apply(f.c, Options{History: &choice, ExpectedObservation: r.Observation, BeforeWrite: func(n int, op Operation) error {
					if n == 1 {
						if mode == "later-edit" {
							must(t, os.WriteFile(op.File, []byte("external edit"), 0600))
						}
						return errors.New("injected")
					}
					return nil
				}})
				if err == nil {
					t.Fatal("failure not returned")
				}
				if mode == "rollback" {
					f.expect("CLAUDE.md", "new version")
					f.expect("AGENTS.md", "new version")
					f.missing("state/pending.json")
				} else {
					contains(t, err, "later edit")
				}
				return
			}
			_, err = RestoreReviewed(f.path("config.json"), r.Observation, choice)
			if mode == "restore" {
				must(t, err)
				f.expect("CLAUDE.md", "one")
				f.expect("AGENTS.md", "one")
				before = auditTree(t, f.dir)
				f.apply()
				if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
					t.Fatal("not idempotent")
				}
			} else {
				if err == nil {
					t.Fatal("unsafe restore accepted")
				}
				if strings.HasPrefix(mode, "stale-") && !errors.Is(err, ErrObservationChanged) {
					t.Fatal(err)
				}
				f.expect("AGENTS.md", "new version")
			}
		})
	}
}

func TestHistoryPreservesCurrentInstructionOverlays(t *testing.T) {
	f := instructionsFixture(t)
	tx := initialHistory(t, f)
	f.write("claude-source", strings.ReplaceAll(f.read("claude-source"), "Shared guidance.", "Updated guidance."))
	f.apply()
	f.write("codex-source", "Current Codex overlay\n"+f.read("codex-source"))
	choice := HistoryChoice{tx, "portable", "codex", "after"}
	r, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), r.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("codex-source"), "Current Codex overlay") || !strings.Contains(f.read("codex-source"), "Shared guidance.") {
		t.Fatal("lost current overlay or historical content")
	}
}

func TestHistoryReadOnlyEmptyAndInvalid(t *testing.T) {
	f := newFixture(t)
	h, err := History(f.path("config.json"))
	must(t, err)
	if len(h) != 0 {
		t.Fatal(h)
	}
	f.missing("state")
	if _, err := RestoreReviewed(f.path("config.json"), "", HistoryChoice{}); err == nil {
		t.Fatal("missing token accepted")
	}
	f.missing("state")
	f.write("state/backups/unexpected/journal.json", "{}")
	if _, err := History(f.path("config.json")); err == nil {
		t.Fatal("invalid history accepted")
	}
}

func TestHistoricalJournalValidation(t *testing.T) {
	for _, mode := range []string{"version", "duplicate-target", "foreign-target", "invalid-mode", "invalid-data", "nil-after"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			tx := initialHistory(t, f)
			j, _, err := readHistoryJournal(f.c, tx)
			must(t, err)
			switch mode {
			case "version":
				j.Version = 9
			case "duplicate-target":
				j.Operations = append(j.Operations, j.Operations[0])
			case "foreign-target":
				j.Operations[0].File = f.path("not-managed")
			case "invalid-mode":
				j.Operations[0].After.Mode = 07777
			case "invalid-data":
				j.Operations[0].After.Data = "not-base64"
			case "nil-after":
				j.Operations[0].After = nil
			}
			must(t, writeJSON(filepath.Join(f.c.StateDir, "backups", tx, "journal.json"), j))
			if _, err := History(f.path("config.json")); err == nil {
				t.Fatal("invalid journal accepted")
			}
			f.expect("AGENTS.md", "one")
		})
	}
}

func TestHistoryCannotBypassOwnership(t *testing.T) {
	f := rosterFixture(t)
	tx := initialHistory(t, f)
	choice := HistoryChoice{tx, "rules", "codex", "after"}
	r, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	f.write("coordination/profiles.json", "{}")
	_, err = RestoreReviewed(f.path("config.json"), r.Observation, choice)
	contains(t, err, "ownership")
}
