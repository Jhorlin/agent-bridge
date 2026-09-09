package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func supportingFixture(t *testing.T) (*fixture, FileChange) {
	f := skillFixture(t)
	f.apply()
	return f, FileChange{Key: "demo/scripts/run.sh"}
}

func TestSupportingFileChange(t *testing.T) {
	for _, rename := range []string{"", "scripts/new/run.sh"} {
		t.Run(rename, func(t *testing.T) {
			f, choice := supportingFixture(t)
			choice.Rename = rename
			modes := map[string]os.FileMode{}
			for _, side := range sides {
				info, err := os.Stat(filepath.Join(f.c.Resources[0].Paths[side], "scripts/run.sh"))
				must(t, err)
				modes[side] = info.Mode().Perm()
			}
			before := auditTree(t, f.dir)
			review, err := ReviewFileChange(f.path("config.json"), choice)
			must(t, err)
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("review wrote files")
			}
			result, err := ApplyFileChange(f.path("config.json"), review.Observation, choice)
			must(t, err)
			if result.Status != "committed" {
				t.Fatal(result)
			}
			for _, side := range sides {
				root := f.c.Resources[0].Paths[side]
				if _, err := os.Lstat(filepath.Join(root, "scripts/run.sh")); !os.IsNotExist(err) {
					t.Fatal("old file retained", err)
				}
				if rename != "" {
					info, err := os.Stat(filepath.Join(root, rename))
					must(t, err)
					if info.Mode().Perm() != modes[side] {
						t.Fatal("executable mode lost", info.Mode())
					}
				}
			}
			f.apply() // The normal watcher must neither resurrect nor re-conflict.
			f.expect("claude-skill/SKILL.md", "---\nname: demo\ndescription: A portable demo.\n---\nUse scripts/run.sh.\n")
			if _, err := os.Stat(f.path("state/file-change-backups/" + result.Transaction + "/journal.json")); err != nil {
				t.Fatal("backup missing", err)
			}
		})
	}
}

func TestSupportingFileRollbackAtEveryWrite(t *testing.T) {
	for _, rename := range []string{"", "scripts/new.sh"} {
		count := 4
		if rename != "" {
			count = 7
		}
		for fail := 0; fail < count; fail++ {
			f, choice := supportingFixture(t)
			choice.Rename = rename
			before := f.read("state/manifest.json")
			review, err := ReviewFileChange(f.path("config.json"), choice)
			must(t, err)
			_, err = applyFileChange(f.path("config.json"), review.Observation, choice, func(index int, _ Operation) error {
				if index == fail {
					return errors.New("injected")
				}
				return nil
			})
			contains(t, err, "rolled back")
			f.expect("state/manifest.json", before)
			for _, side := range sides {
				root := f.c.Resources[0].Paths[side]
				if _, err := os.Stat(filepath.Join(root, "scripts/run.sh")); err != nil {
					t.Fatal("rollback lost original", err)
				}
				if rename != "" {
					if _, err := os.Stat(filepath.Join(root, rename)); !os.IsNotExist(err) {
						t.Fatal("rollback retained new file", err)
					}
				}
			}
			f.missing("state/file-change-pending.json")
		}
	}
}

func TestSupportingFileInterruptedRecoveryAndLaterEdits(t *testing.T) {
	for _, mode := range []string{"recover", "later-edit", "identity", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			f, choice := supportingFixture(t)
			choice.Rename = "scripts/new.sh"
			review, err := ReviewFileChange(f.path("config.json"), choice)
			must(t, err)
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("expected interruption")
					}
				}()
				_, _ = applyFileChange(f.path("config.json"), review.Observation, choice, func(index int, _ Operation) error {
					if index == 2 {
						panic("fixture interruption")
					}
					return nil
				})
			}()
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("normal sync ignored interrupted deletion")
			}
			if _, err := Recover(f.c); err == nil {
				t.Fatal("normal recovery ignored interrupted deletion")
			}
			newPath := filepath.Join(f.c.Resources[0].Paths["shared"], choice.Rename)
			if mode == "later-edit" {
				must(t, os.WriteFile(newPath, []byte("external edit"), 0600))
			}
			if mode == "identity" {
				f.raw.Resources[0].Scope = "project"
				f.load()
			}
			if mode == "ambiguous" {
				var pointer Pending
				raw, err := snapshot(fileChangePending(f.c))
				must(t, err)
				must(t, decode(raw, &pointer))
				path := f.path("state/file-change-backups/" + pointer.Transaction + "/journal.json")
				raw, err = snapshot(path)
				must(t, err)
				var journal fileChangeJournal
				must(t, decodeEnrollmentJSON(raw, &journal))
				journal.Created[0] = false
				must(t, writeJSON(path, journal))
			}
			_, err = RecoverFileChange(f.path("config.json"))
			if mode == "recover" {
				must(t, err)
				f.apply()
			} else if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if mode == "later-edit" {
				data, err := os.ReadFile(newPath)
				must(t, err)
				if string(data) != "external edit" {
					t.Fatal("lost later edit")
				}
			}
		})
	}
}

func TestSupportingFileChangeRefusals(t *testing.T) {
	for _, choice := range []FileChange{{Key: "demo/SKILL.md"}, {Key: "demo/scripts/run.sh", Rename: "SKILL.md"}, {Key: "demo/scripts/run.sh", Rename: "scripts/../assets/file"}, {Key: "demo/scripts/run.sh", Rename: "scripts/RUN.sh"}, {Key: "unknown/scripts/run.sh"}} {
		f, _ := supportingFixture(t)
		if _, err := ReviewFileChange(f.path("config.json"), choice); err == nil {
			t.Fatal("unsafe choice accepted", choice)
		}
	}
	f, choice := supportingFixture(t)
	review, err := ReviewFileChange(f.path("config.json"), choice)
	must(t, err)
	f.write("claude-skill/scripts/run.sh", "later edit")
	if _, err := ApplyFileChange(f.path("config.json"), review.Observation, choice); err == nil {
		t.Fatal("stale deletion accepted")
	}
	f.expect("claude-skill/scripts/run.sh", "later edit")
}

func TestSupportingRenameDoesNotOwnExternalCreation(t *testing.T) {
	f, choice := supportingFixture(t)
	choice.Rename = "scripts/new.sh"
	review, err := ReviewFileChange(f.path("config.json"), choice)
	must(t, err)
	var created string
	_, err = applyFileChange(f.path("config.json"), review.Observation, choice, func(index int, op Operation) error {
		if index == 0 {
			created = op.File
			must(t, writeSnapshot(created, op.After)) // Even identical bytes aren't ours.
			return errors.New("external creator won")
		}
		return nil
	})
	contains(t, err, "ambiguous")
	if _, err := os.Stat(created); err != nil {
		t.Fatal("external file removed", err)
	}
	if _, err := RecoverFileChange(f.path("config.json")); err == nil {
		t.Fatal("ambiguous ownership recovery accepted")
	}
}
