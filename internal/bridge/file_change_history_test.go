package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func changedSupportingFixture(t *testing.T, rename string) (*fixture, string, map[string]*Snapshot) {
	f, choice := supportingFixture(t)
	choice.Rename = rename
	old := map[string]*Snapshot{}
	for _, side := range sides {
		var err error
		old[side], err = snapshot(filepath.Join(f.c.Resources[0].Paths[side], "scripts/run.sh"))
		must(t, err)
	}
	review, err := ReviewFileChange(f.path("config.json"), choice)
	must(t, err)
	r, err := ApplyFileChange(f.path("config.json"), review.Observation, choice)
	must(t, err)
	return f, r.Transaction, old
}

func TestSupportingFileUndo(t *testing.T) {
	for _, rename := range []string{"", "scripts/new/run.sh"} {
		t.Run(rename, func(t *testing.T) {
			f, tx, old := changedSupportingFixture(t, rename)
			f.write("claude-skill/references/later.txt", "unrelated newer work")
			f.apply()
			before := auditTree(t, f.dir)
			entries, err := FileChangeHistory(f.path("config.json"))
			must(t, err)
			if len(entries) != 1 || entries[0].Transaction != tx {
				t.Fatal(entries)
			}
			review, err := ReviewFileChangeUndo(f.path("config.json"), tx)
			must(t, err)
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("history review wrote files")
			}
			result, err := ApplyFileChangeUndo(f.path("config.json"), review.Observation, tx)
			must(t, err)
			if result.Status != "committed" {
				t.Fatal(result)
			}
			for _, side := range sides {
				root := f.c.Resources[0].Paths[side]
				now, err := snapshot(filepath.Join(root, "scripts/run.sh"))
				must(t, err)
				if !equal(now, old[side]) {
					t.Fatal("restoration lost original bytes/mode", side)
				}
				if rename != "" {
					now, err := snapshot(filepath.Join(root, rename))
					must(t, err)
					if now != nil {
						t.Fatal("undo retained renamed destination")
					}
				}
			}
			f.apply()
			f.expect("codex-skill/references/later.txt", "unrelated newer work")
			if _, err := ReviewFileChangeUndo(f.path("config.json"), tx); err == nil {
				t.Fatal("undo silently repeated")
			}
			entries, err = FileChangeHistory(f.path("config.json"))
			must(t, err)
			if len(entries) != 2 {
				t.Fatal(entries)
			}
		})
	}
}

func TestSupportingFileUndoEveryWriteRollback(t *testing.T) {
	for _, rename := range []string{"", "scripts/new.sh"} {
		count := 4
		if rename != "" {
			count = 7
		}
		for fail := 0; fail < count; fail++ {
			f, tx, _ := changedSupportingFixture(t, rename)
			manifest := f.read("state/manifest.json")
			review, err := ReviewFileChangeUndo(f.path("config.json"), tx)
			must(t, err)
			_, err = applyFileChangeUndo(f.path("config.json"), review.Observation, tx, func(index int, _ Operation) error {
				if index == fail {
					return errors.New("injected")
				}
				return nil
			})
			contains(t, err, "rolled back")
			f.expect("state/manifest.json", manifest)
			for _, side := range sides {
				now, err := snapshot(filepath.Join(f.c.Resources[0].Paths[side], "scripts/run.sh"))
				must(t, err)
				if now != nil {
					t.Fatal("failed undo left restored file")
				}
			}
			f.missing("state/file-change-pending.json")
			f.apply()
		}
	}
}

func TestSupportingFileUndoInterruption(t *testing.T) {
	for _, mode := range []string{"recover", "later-edit", "ambiguous", "identity"} {
		t.Run(mode, func(t *testing.T) {
			f, tx, _ := changedSupportingFixture(t, "")
			review, err := ReviewFileChangeUndo(f.path("config.json"), tx)
			must(t, err)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("expected interruption")
					}
				}()
				_, _ = applyFileChangeUndo(f.path("config.json"), review.Observation, tx, func(index int, _ Operation) error {
					if index == 1 {
						panic("fixture interruption")
					}
					return nil
				})
			}()
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("normal sync ignored interrupted undo")
			}
			if mode == "later-edit" {
				f.write("state/shared/demo/scripts/run.sh", "later edit")
			}
			if mode == "identity" {
				f.raw.Resources[0].Scope = "project"
				f.load()
			}
			if mode == "ambiguous" {
				var pending Pending
				s, err := snapshot(fileChangePending(f.c))
				must(t, err)
				must(t, decode(s, &pending))
				path := f.path("state/file-change-backups/" + pending.Transaction + "/journal.json")
				s, err = snapshot(path)
				must(t, err)
				var j fileChangeJournal
				must(t, decodeEnrollmentJSON(s, &j))
				j.Created[0] = false
				must(t, writeJSON(path, j))
			}
			r, err := RecoverFileChange(f.path("config.json"))
			if mode == "recover" {
				must(t, err)
				if r.Status != "recovered" {
					t.Fatal(r)
				}
				f.apply()
			} else if err == nil {
				t.Fatal("unsafe undo recovery accepted")
			}
			if mode == "later-edit" {
				f.expect("state/shared/demo/scripts/run.sh", "later edit")
			}
		})
	}
}

func TestSupportingFileUndoRefusals(t *testing.T) {
	for _, mode := range []string{"occupied", "case", "symlink", "stale-journal", "corrupt-journal", "stale-input", "identity"} {
		t.Run(mode, func(t *testing.T) {
			f, tx, _ := changedSupportingFixture(t, "")
			review, err := ReviewFileChangeUndo(f.path("config.json"), tx)
			must(t, err)
			path := f.path("state/file-change-backups/" + tx + "/journal.json")
			switch mode {
			case "occupied":
				f.write("codex-skill/scripts/run.sh", "external")
			case "case":
				f.write("codex-skill/scripts/RUN.sh", "external")
			case "symlink":
				must(t, os.Symlink(f.path("outside"), f.path("codex-skill/scripts/run.sh")))
			case "stale-journal":
				data, err := os.ReadFile(path)
				must(t, err)
				must(t, os.WriteFile(path, append(data, '\n'), 0600))
			case "corrupt-journal":
				j, _, err := readFileChangeHistory(f.c, tx)
				must(t, err)
				j.Operations[0].File = f.path("outside")
				must(t, writeJSON(path, j))
			case "stale-input":
				f.write("claude-skill/references/new.txt", "later edit")
			case "identity":
				f.raw.Resources[0].Scope = "project"
				f.load()
			}
			if _, err := ApplyFileChangeUndo(f.path("config.json"), review.Observation, tx); err == nil {
				t.Fatal("unsafe historical undo accepted")
			}
			f.missing("state/file-change-pending.json")
		})
	}
}

func TestSupportingFileUndoKeepsExternalCreation(t *testing.T) {
	f, tx, _ := changedSupportingFixture(t, "")
	review, err := ReviewFileChangeUndo(f.path("config.json"), tx)
	must(t, err)
	var created string
	_, err = applyFileChangeUndo(f.path("config.json"), review.Observation, tx, func(index int, op Operation) error {
		if index == 0 {
			created = op.File
			must(t, writeSnapshot(created, op.After))
			return errors.New("external creator won")
		}
		return nil
	})
	contains(t, err, "ambiguous")
	if _, err := os.Stat(created); err != nil {
		t.Fatal("external creation removed", err)
	}
}
