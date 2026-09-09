package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func rosterFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	writeRoster(t, f, []string{f.path("config.json")})
	return f
}

func writeRoster(t *testing.T, f *fixture, files []string) {
	t.Helper()
	data, err := json.Marshal(ownershipRoster{Version: 1, Profiles: files})
	must(t, err)
	f.write("coordination/profiles.json", string(data))
}

func TestOwnershipEnforcement(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		f := rosterFixture(t)
		other := configInput{Version: 1, StateDir: "other-state", CoordinationDir: "coordination", Resources: []resourceInput{}}
		if overlap {
			other.Resources = f.raw.Resources
		}
		data, err := json.Marshal(other)
		must(t, err)
		f.write("other.json", string(data))
		writeRoster(t, f, []string{f.path("config.json"), f.path("other.json")})
		_, err = Apply(f.c, Options{})
		if overlap {
			contains(t, err, "ownership overlaps")
			f.missing("AGENTS.md")
			f.missing("state")
			_, err = Recover(f.c)
			contains(t, err, "ownership overlaps")
		} else {
			must(t, err)
			f.expect("AGENTS.md", "one")
			f.apply()
		}
		f.missing("coordination/sync.lock")
	}
}

func TestOwnershipInvalidRosters(t *testing.T) {
	for _, data := range []string{`{}`, `{"version":2,"profiles":[]}`, `{"version":1,"profiles":["relative"]}`, `{"version":1,"version":1,"profiles":[]}`, `{"version":1,"profiles":[],"secret":"DO_NOT_PRINT"}`} {
		f := rosterFixture(t)
		f.write("coordination/profiles.json", data)
		_, err := Apply(f.c, Options{})
		contains(t, err, "ownership")
		f.missing("state")
		f.missing("AGENTS.md")
	}
	f := rosterFixture(t)
	writeRoster(t, f, []string{f.path("missing.json")})
	_, err := Apply(f.c, Options{})
	contains(t, err, "missing")
	writeRoster(t, f, []string{f.path("config.json"), f.path("config.json")})
	_, err = Apply(f.c, Options{})
	contains(t, err, "distinct")
	must(t, os.Remove(f.path("coordination/profiles.json")))
	must(t, os.Symlink(f.path("config.json"), f.path("coordination/profiles.json")))
	_, err = Apply(f.c, Options{})
	contains(t, err, "unsafe")
}

func TestOwnershipStaleConfigurationAndRecovery(t *testing.T) {
	f := rosterFixture(t)
	f.raw.Resources[0].Claude = "new-source"
	f.save()
	_, err := Apply(f.c, Options{})
	contains(t, err, "changed")
	f.raw.Resources[0].Claude = "CLAUDE.md"
	f.load()
	_, err = Apply(f.c, Options{BeforeWrite: func(int, Operation) error { return errors.New("injected") }})
	contains(t, err, "injected")
	_, err = Recover(f.c)
	must(t, err)
	f.apply()
	f.expect("AGENTS.md", "one")
}

func TestOwnershipMembershipAndCoordinator(t *testing.T) {
	f := rosterFixture(t)
	other := configInput{Version: 1, StateDir: "other-state", CoordinationDir: "coordination", Resources: []resourceInput{}}
	data, err := json.Marshal(other)
	must(t, err)
	f.write("other.json", string(data))
	writeRoster(t, f, []string{f.path("other.json")})
	_, err = Apply(f.c, Options{})
	contains(t, err, "not enrolled")
	other.CoordinationDir = "different-coordinator"
	data, err = json.Marshal(other)
	must(t, err)
	f.write("other.json", string(data))
	writeRoster(t, f, []string{f.path("config.json"), f.path("other.json")})
	_, err = Apply(f.c, Options{})
	contains(t, err, "same coordinator")
	f.missing("state")
	f.missing("AGENTS.md")
}

func TestOwnershipRetainsPendingRecovery(t *testing.T) {
	f := rosterFixture(t)
	f.apply()
	f.write("CLAUDE.md", "new")
	_, err := Apply(f.c, Options{BeforeWrite: func(i int, _ Operation) error {
		if i == 1 {
			f.write("state/shared/rules", "later edit")
			return errors.New("injected")
		}
		return nil
	}})
	contains(t, err, "pending transaction retained")
	pending := f.read("state/pending.json")
	roster := f.read("coordination/profiles.json")
	f.write("coordination/profiles.json", `{}`)
	_, err = Recover(f.c)
	contains(t, err, "ownership")
	f.expect("state/pending.json", pending)
	f.expect("state/shared/rules", "later edit")
	f.write("coordination/profiles.json", roster)
	_, err = Recover(f.c)
	contains(t, err, "later edit")
	f.write("state/shared/rules", "new")
	r, err := Recover(f.c)
	must(t, err)
	if r.Status != "recovered" {
		t.Fatal(r)
	}
	f.expect("AGENTS.md", "one")
	f.expect("state/shared/rules", "one")
	f.missing("state/pending.json")
}
