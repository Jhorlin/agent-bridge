package bridge

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestReviewedSyncFreshness(t *testing.T) {
	for _, change := range []string{"none", "source", "destination", "profile", "manifest"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			r, err := ReviewProfile(f.path("config.json"))
			must(t, err)
			f.missing("state")
			f.missing("AGENTS.md")
			if !r.ReadOnly || !digestPattern.MatchString(r.Observation) {
				t.Fatal(r)
			}
			switch change {
			case "source":
				f.write("CLAUDE.md", "changed")
			case "destination":
				f.write("AGENTS.md", "one")
			case "profile":
				f.raw.StateDir = "other-state"
				f.save()
			case "manifest":
				f.apply()
			}
			_, err = SyncReviewed(f.path("config.json"), r.Observation)
			if change == "none" {
				must(t, err)
				f.expect("AGENTS.md", "one")
			} else {
				if !errors.Is(err, ErrObservationChanged) {
					t.Fatalf("expected stale review, got %v", err)
				}
				if change == "source" {
					f.expect("CLAUDE.md", "changed")
					f.missing("AGENTS.md")
				}
			}
		})
	}
}

func TestReviewRefusesInvalidInputsAndOwnership(t *testing.T) {
	f := newFixture(t)
	for _, token := range []string{"", "not-a-digest", strings.Repeat("a", 63)} {
		if _, err := SyncReviewed(f.path("config.json"), token); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	f.missing("state")
	f.write("AGENTS.md", "conflicting")
	if _, err := ReviewProfile(f.path("config.json")); err == nil {
		t.Fatal("conflict reviewed")
	}
	g := rosterFixture(t)
	r, err := ReviewProfile(g.path("config.json"))
	must(t, err)
	g.write("coordination/profiles.json", `{}`)
	_, err = SyncReviewed(g.path("config.json"), r.Observation)
	contains(t, err, "ownership")
	g.missing("AGENTS.md")
	g.missing("state")
}

func TestReviewOutputDoesNotExposeContents(t *testing.T) {
	f := newFixture(t)
	f.write("CLAUDE.md", "PRIVATE-CONTENT-SENTINEL")
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	data, err := json.Marshal(r)
	must(t, err)
	if strings.Contains(string(data), "PRIVATE-CONTENT") {
		t.Fatal("leaked native content")
	}
}
