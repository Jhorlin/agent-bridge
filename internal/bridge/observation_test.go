package bridge

import (
	"errors"
	"strings"
	"testing"
)

func TestObservedApplyRejectsChangedSourceBeforeJournal(t *testing.T) {
	f := newFixture(t)
	observed := Observation(f.c, f.plan())
	if observed != Observation(f.c, f.plan()) {
		t.Fatal("observation is nondeterministic")
	}
	f.write("CLAUDE.md", "newer edit")
	_, err := Apply(f.c, Options{ExpectedObservation: observed})
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatalf("expected changed observation: %v", err)
	}
	f.expect("CLAUDE.md", "newer edit")
	f.missing("AGENTS.md")
	f.missing("state/backups")
	f.missing("state/pending.json")
	f.missing("state/manifest.json")
	f.missing("state/sync.lock")
	_, err = Apply(f.c, Options{ExpectedObservation: Observation(f.c, f.plan())})
	must(t, err)
	f.expect("AGENTS.md", "newer edit")
}

func TestObservationIncludesRawInputsAndProfile(t *testing.T) {
	f := mcpFixture(t)
	before := Observation(f.c, f.plan())
	f.write("claude.json", f.read("claude.json")+"\n")
	if Observation(f.c, f.plan()) == before {
		t.Fatal("raw formatting change was not observed")
	}
	p := f.plan()
	before = Observation(f.c, p)
	changed := f.c
	changed.CoordinationDir = f.path("other-coordinator")
	if Observation(changed, p) == before {
		t.Fatal("profile change was not observed")
	}
}

func TestPrepareRaceIsRetryableBeforeAnyJournal(t *testing.T) {
	for _, kind := range []string{"ordinary", "instruction-companion", "instruction-codex"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			path := "CLAUDE.md"
			if kind != "ordinary" {
				f = instructionSetFixture(t, false)
				f.apply()
				path = ".claude/CLAUDE.md"
				if kind == "instruction-codex" {
					path = "AGENTS.md"
				}
			}
			before := f.plan()
			var newest string
			_, err := Apply(f.c, Options{ExpectedObservation: Observation(f.c, before), beforePrepare: func() {
				newest = f.read(path)
				if kind == "instruction-codex" {
					newest = strings.Replace(newest, "alternate\n", "newest alternate\n", 1)
				} else {
					newest += "\nnewest edit"
				}
				f.write(path, newest)
			}})
			if !errors.Is(err, ErrObservationChanged) {
				t.Fatalf("pre-journal edit must tell watcher to retry, got: %v", err)
			}
			f.expect(path, newest)
			f.missing("state/pending.json")
			f.missing("state/sync.lock")
			after := f.plan()
			if !equal(before.ManifestBefore, after.ManifestBefore) {
				t.Fatal("stale plan changed manifest")
			}
			_, err = Apply(f.c, Options{ExpectedObservation: Observation(f.c, after)})
			must(t, err)
			for _, item := range f.plan().Items {
				if len(item.Writes) != 0 || item.Conflict != "" {
					t.Fatal("retry failed to converge")
				}
			}
		})
	}
}
