package bridge

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCheckOverlaps(t *testing.T) {
	for _, tc := range []struct {
		name, state, claude, codex string
		want                       bool
	}{
		{"independent", "other-state", "other-a", "other-b", false},
		{"same resource", "other-state", "CLAUDE.md", "other-b", true},
		{"nested resource", "other-state", "CLAUDE.md/child", "other-b", true},
		{"case collision", "other-state", "claude.md", "other-b", true},
		{"shared state", "state", "other-a", "other-b", true},
		{"resource overwrites config", "other-state", "config.json", "other-b", true},
		{"resource overwrites state", "other-state", "state/shared", "other-b", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			// The nesting case uses a missing directory candidate, not a file parent.
			must(t, os.Remove(f.path("CLAUDE.md")))
			other := configInput{Version: 1, StateDir: tc.state, Resources: []resourceInput{{ID: "other", Kind: "portable-file", Scope: "project", Claude: tc.claude, Codex: tc.codex}}}
			data, err := json.Marshal(other)
			must(t, err)
			f.write("other.json", string(data))
			r, err := CheckOverlaps([]string{f.path("config.json"), f.path("other.json")})
			must(t, err)
			if (len(r.Overlaps) > 0) != tc.want || !r.ReadOnly {
				t.Fatalf("unexpected report: %+v", r)
			}
			f.missing("state")
			f.missing("other-state")
		})
	}
}

func TestCheckOverlapsInheritanceAndInvalidInputs(t *testing.T) {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.save()
	child := configInput{Version: 1, StateDir: "other-state", Extends: "config.json", Resources: []resourceInput{}, Disable: []string{f.raw.Resources[0].ID}}
	data, err := json.Marshal(child)
	must(t, err)
	f.write("child.json", string(data))
	r, err := CheckOverlaps([]string{f.path("config.json"), f.path("child.json")})
	must(t, err)
	if len(r.Overlaps) != 0 {
		t.Fatalf("shared config/coordinator not an overlap: %+v", r)
	}
	for _, files := range [][]string{nil, {f.path("config.json")}, {f.path("config.json"), f.path("config.json")}, {f.path("config.json"), f.path("missing")}} {
		if _, err := CheckOverlaps(files); err == nil {
			t.Fatal("accepted invalid inputs")
		}
	}
	must(t, os.Symlink(f.path("config.json"), f.path("linked.json")))
	if _, err := CheckOverlaps([]string{f.path("config.json"), f.path("linked.json")}); err == nil {
		t.Fatal("accepted symlink profile")
	}
	f.missing("coordination")
	f.missing("state")
}

func TestCheckOverlapsPinnedTargetsAndNestedCoordinators(t *testing.T) {
	f := newFixture(t)
	f.write("target", "one")
	must(t, os.Remove(f.path("CLAUDE.md")))
	must(t, os.Symlink(f.path("target"), f.path("CLAUDE.md")))
	f.raw.Resources[0].LinkTargets = map[string]string{"claude": "target"}
	f.raw.CoordinationDir = "coordination"
	f.save()
	other := configInput{Version: 1, StateDir: "other-state", CoordinationDir: "coordination/nested", Resources: []resourceInput{{ID: "other", Kind: "portable-file", Scope: "global", Claude: "target", Codex: "other-b"}}}
	data, err := json.Marshal(other)
	must(t, err)
	f.write("other.json", string(data))
	r, err := CheckOverlaps([]string{f.path("config.json"), f.path("other.json")})
	must(t, err)
	if len(r.Overlaps) != 2 {
		t.Fatalf("expected target and coordinator conflicts: %+v", r)
	}
	f.missing("coordination")
	f.missing("state")
	f.expect("target", "one")
}
