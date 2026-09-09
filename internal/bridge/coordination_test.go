package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCoordinationAcrossProfiles(t *testing.T) {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	other := f.c
	other.StateDir = f.path("other-state")
	err := locked(f.c, func() error {
		_, err := Apply(other, Options{})
		contains(t, err, "another sync")
		_, err = Recover(other)
		contains(t, err, "another sync")
		f.missing("other-state")
		return nil
	})
	must(t, err)
	f.missing("coordination/sync.lock")
	f.apply()
	f.expect("AGENTS.md", "one")
}

func TestCoordinationReleasesOnFailure(t *testing.T) {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	want := errors.New("injected failure")
	if err := locked(f.c, func() error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	f.missing("coordination/sync.lock")
	f.missing("state/sync.lock")
	f.write("state/sync.lock", "stale")
	contains(t, locked(f.c, func() error { t.Fatal("entered locked state"); return nil }), "another sync")
	f.missing("coordination/sync.lock")
	f.expect("state/sync.lock", "stale")
}

func TestCoordinationInheritanceAndSafety(t *testing.T) {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	child := configInput{Version: 1, StateDir: "state", Extends: "../config.json", Resources: []resourceInput{}}
	data, err := json.Marshal(child)
	must(t, err)
	f.write("project/bridge.json", string(data))
	c, err := LoadAuditConfig(f.path("project/bridge.json"))
	must(t, err)
	if c.CoordinationDir != f.path("coordination") {
		t.Fatal(c.CoordinationDir)
	}
	_, err = Plan(c)
	must(t, err)
	f.missing("coordination")
	for _, path := range []string{"state", "state/nested", "CLAUDE.md", ".", "config.json", "~/locks"} {
		f.raw.CoordinationDir = path
		f.save()
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatalf("accepted unsafe path %s", path)
		}
	}
	must(t, os.Symlink(f.path("other"), f.path("linked")))
	f.raw.CoordinationDir = "linked"
	f.save()
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestCoordinationChildProcess(t *testing.T) {
	if path := os.Getenv("AGENT_BRIDGE_COORDINATION_TEST"); path != "" {
		c, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		contains(t, locked(c, func() error { t.Fatal("child entered locked coordinator"); return nil }), "another sync")
		return
	}
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	child := f.raw
	child.StateDir = "child-state"
	data, err := json.Marshal(child)
	must(t, err)
	f.write("child.json", string(data))
	must(t, locked(f.c, func() error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinationChildProcess$")
		cmd.Env = append(os.Environ(), "AGENT_BRIDGE_COORDINATION_TEST="+filepath.Join(f.dir, "child.json"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v %s", err, output)
		}
		return nil
	}))
	f.missing("child-state")
}
