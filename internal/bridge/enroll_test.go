package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestEnrollReviewedCreatesRosterWithoutSync(t *testing.T) {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	must(t, EnrollReviewed(f.path("config.json"), r.Observation))
	f.missing("state")
	f.missing("AGENTS.md")
	f.expect("CLAUDE.md", "one")
	before := f.read("coordination/profiles.json")
	must(t, EnrollReviewed(f.path("config.json"), r.Observation))
	f.expect("coordination/profiles.json", before)
	files, err := os.ReadDir(f.path("coordination/enrollment-backups"))
	must(t, err)
	if len(files) != 1 {
		t.Fatal("idempotent enrollment wrote another backup")
	}
	_, err = SyncReviewed(f.path("config.json"), r.Observation)
	must(t, err)
	f.expect("AGENTS.md", "one")
}

func TestEnrollmentExistingRosterAndOverlap(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		f := rosterFixture(t)
		other := configInput{Version: 1, StateDir: "other-state", CoordinationDir: "coordination", Resources: []resourceInput{}}
		if overlap {
			other.Resources = f.raw.Resources
		}
		data, err := json.Marshal(other)
		must(t, err)
		f.write("other.json", string(data))
		r, err := ReviewProfile(f.path("other.json"))
		must(t, err)
		before := f.read("coordination/profiles.json")
		err = EnrollReviewed(f.path("other.json"), r.Observation)
		if overlap {
			contains(t, err, "overlaps")
			f.expect("coordination/profiles.json", before)
		} else {
			must(t, err)
			var roster ownershipRoster
			must(t, json.Unmarshal([]byte(f.read("coordination/profiles.json")), &roster))
			if len(roster.Profiles) != 2 {
				t.Fatal(roster)
			}
		}
		f.missing("state")
		f.missing("other-state")
		f.missing("AGENTS.md")
	}
}

func TestEnrollmentRejectsStaleAndUnsafeInputs(t *testing.T) {
	f := newFixture(t)
	if err := EnrollReviewed(f.path("config.json"), ""); err == nil {
		t.Fatal("empty review accepted")
	}
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	contains(t, EnrollReviewed(f.path("config.json"), r.Observation), "coordinationDir")
	f.raw.CoordinationDir = "coordination"
	f.load()
	r, err = ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("CLAUDE.md", "changed")
	if err := EnrollReviewed(f.path("config.json"), r.Observation); !errors.Is(err, ErrObservationChanged) {
		t.Fatal(err)
	}
	f.missing("coordination/profiles.json")
	f.write("CLAUDE.md", "one")
	f.write("coordination/profiles.json", `{"version":1,"profiles":[],"unknown":"secret"}`)
	contains(t, EnrollReviewed(f.path("config.json"), r.Observation), "invalid roster")
	f.missing("state")
	f.missing("AGENTS.md")
	must(t, os.Remove(f.path("coordination/profiles.json")))
	must(t, os.Symlink(f.path("config.json"), f.path("coordination/profiles.json")))
	if err := EnrollReviewed(f.path("config.json"), r.Observation); err == nil {
		t.Fatal("symlink roster accepted")
	}
}

func TestExclusiveRosterCreationPreservesExistingFile(t *testing.T) {
	f := newFixture(t)
	f.write("roster.json", "later edit")
	after, err := encoded(ownershipRoster{Version: 1, Profiles: []string{f.path("config.json")}})
	must(t, err)
	if err := createRosterExclusive(f.path("roster.json"), after); err == nil {
		t.Fatal("overwrote concurrent roster")
	}
	f.expect("roster.json", "later edit")
}
