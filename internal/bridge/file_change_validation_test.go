package bridge

import (
	"reflect"
	"testing"
)

func TestFileChangeCorruptJournalNeverWrites(t *testing.T) {
	for _, mode := range []string{"delete", "rename", "restore"} {
		t.Run(mode, func(t *testing.T) {
			f, choice := supportingFixture(t)
			var original fileChangeJournal
			var err error
			if mode == "rename" {
				choice.Rename = "scripts/renamed.sh"
			}
			if mode == "restore" {
				var tx string
				f, tx, _ = changedSupportingFixture(t, "")
				original, _, err = prepareFileChangeUndo(f.c, tx)
			} else {
				original, _, err = prepareFileChange(f.c, choice)
			}
			must(t, err)
			must(t, validateFileChangeJournal(f.c, original))
			mutateManifest := func(j *fileChangeJournal, before bool, change func(*Manifest)) {
				op := &j.Operations[len(j.Operations)-1]
				s := op.After
				if before {
					s = op.Before
				}
				var m Manifest
				must(t, decodeEnrollmentJSON(s, &m))
				change(&m)
				value, err := encoded(m)
				must(t, err)
				if before {
					op.Before = value
				} else {
					op.After = value
				}
			}
			cases := map[string]func(*fileChangeJournal){
				"version":          func(j *fileChangeJournal) { j.Version = 2 },
				"identity":         func(j *fileChangeJournal) { j.Resource.Scope = "changed" },
				"entrypoint":       func(j *fileChangeJournal) { j.Change.Key = "demo/SKILL.md" },
				"operation-count":  func(j *fileChangeJournal) { j.Operations = j.Operations[:1] },
				"receipt-count":    func(j *fileChangeJournal) { j.Created = nil },
				"target":           func(j *fileChangeJournal) { j.Operations[0].File = f.path("not-owned") },
				"bad-snapshot":     func(j *fileChangeJournal) { j.Operations[0].After = &Snapshot{Data: "invalid-base64"} },
				"manifest-absent":  func(j *fileChangeJournal) { j.Operations[len(j.Operations)-1].Before = nil },
				"manifest-receipt": func(j *fileChangeJournal) { j.Created[len(j.Created)-1] = true },
				"unrelated-manifest": func(j *fileChangeJournal) {
					mutateManifest(j, false, func(m *Manifest) { m.Resources = map[string]Resource{} })
				},
			}
			if mode == "restore" {
				cases["restore-and-rename"] = func(j *fileChangeJournal) { j.Change.Rename = "scripts/new.sh" }
				cases["existing-baseline"] = func(j *fileChangeJournal) {
					mutateManifest(j, true, func(m *Manifest) { m.Files[j.Change.Key] = fingerprint(j.Operations[0].After) })
				}
				cases["missing-restored-baseline"] = func(j *fileChangeJournal) {
					mutateManifest(j, false, func(m *Manifest) { delete(m.Files, j.Change.Key) })
				}
				cases["restore-before"] = func(j *fileChangeJournal) { j.Operations[0].Before = j.Operations[0].After }
				cases["restore-after"] = func(j *fileChangeJournal) { j.Operations[0].After = nil }
			} else {
				cases["missing-baseline"] = func(j *fileChangeJournal) {
					mutateManifest(j, true, func(m *Manifest) { delete(m.Files, j.Change.Key) })
				}
				index := 0
				if mode == "rename" {
					index = 1
				}
				cases["deletion-before"] = func(j *fileChangeJournal) { j.Operations[index].Before = nil }
				cases["deletion-after"] = func(j *fileChangeJournal) { j.Operations[index].After = j.Operations[index].Before }
				cases["removal-receipt"] = func(j *fileChangeJournal) { j.Created[index] = true }
				cases["baseline-fingerprint"] = func(j *fileChangeJournal) { j.Operations[index].Before.Data = "Y2hhbmdlZA==" }
				if mode == "rename" {
					cases["occupied-baseline"] = func(j *fileChangeJournal) {
						mutateManifest(j, true, func(m *Manifest) { m.Files[j.Resource.ID+"/"+j.Change.Rename] = fingerprint(j.Operations[0].After) })
					}
					cases["rename-before"] = func(j *fileChangeJournal) { j.Operations[0].Before = j.Operations[0].After }
					cases["rename-after"] = func(j *fileChangeJournal) { j.Operations[0].After = nil }
				}
			}
			for name, mutate := range cases {
				t.Run(name, func(t *testing.T) {
					copy, err := encoded(original)
					must(t, err)
					var j fileChangeJournal
					must(t, decodeEnrollmentJSON(copy, &j))
					mutate(&j)
					before := auditTree(t, f.dir)
					if validateFileChangeJournal(f.c, j) == nil {
						t.Fatal("corrupt journal validated")
					}
					if rollbackFileChange(f.c, j) == nil {
						t.Fatal("corrupt journal recovered")
					}
					if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
						t.Fatal("corrupt recovery changed files")
					}
				})
			}
		})
	}
}
