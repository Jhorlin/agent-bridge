package bridge

import (
	"errors"
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
