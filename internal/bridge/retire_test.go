package bridge

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestRetirementPreservesFilesAndBlocksStaleWriter(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		f := skillFixture(t)
		if inherited {
			f.write("parent.json", f.read("config.json"))
			f.write("config.json", `{"version":1,"extends":"parent.json","stateDir":"state","resources":[]}`)
			var err error
			f.c, err = LoadConfig(f.path("config.json"))
			must(t, err)
		}
		f.apply()
		must(t, os.Chmod(f.path("config.json"), 0640))
		before := auditTree(t, f.dir)
		review, err := ReviewRetirement(f.path("config.json"), "demo")
		must(t, err)
		if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
			t.Fatal("review mutated files")
		}
		oldProfile := f.read("config.json")
		manifest := f.read("state/manifest.json")
		result, err := ApplyRetirement(f.path("config.json"), review.Observation, "demo")
		must(t, err)
		backup, err := os.ReadFile(result.Backup)
		must(t, err)
		if string(backup) != oldProfile {
			t.Fatal("backup changed bytes")
		}
		info, err := os.Stat(result.Backup)
		must(t, err)
		if info.Mode().Perm() != 0600 {
			t.Fatal("backup not private")
		}
		info, err = os.Stat(f.path("config.json"))
		must(t, err)
		if info.Mode().Perm() != 0640 {
			t.Fatal("profile mode changed")
		}
		_, err = Apply(f.c, Options{})
		contains(t, err, "profile changed")
		current, err := LoadConfig(f.path("config.json"))
		must(t, err)
		if len(current.Resources) != 0 {
			t.Fatal("resource still active")
		}
		_, err = Apply(current, Options{})
		must(t, err)
		f.expect("state/manifest.json", manifest)
		after := auditTree(t, f.dir)
		for path, value := range before {
			if path == f.path("config.json") {
				continue
			}
			if !reflect.DeepEqual(value, after[path]) {
				t.Fatalf("retirement changed %s", path)
			}
		}
		// Exact profile restoration reactivates the retained baseline safely.
		f.write("config.json", oldProfile)
		f.apply()
	}
}

func TestRetirementRefusesStalePendingAndInterruptedWrite(t *testing.T) {
	for _, scenario := range []string{"stale", "pending", "file-pending", "locked", "failure", "race", "unknown", "link"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.apply()
			review, err := ReviewRetirement(f.path("config.json"), "rules")
			must(t, err)
			original := f.read("config.json")
			var hook func() error
			switch scenario {
			case "stale":
				f.write("config.json", original+"\n")
			case "pending":
				f.write("state/pending.json", `{}`)
			case "file-pending":
				f.write("state/file-change-pending.json", `{}`)
			case "locked":
				f.write("state/sync.lock", `{}`)
			case "failure":
				hook = func() error { return errors.New("injected failure") }
			case "race":
				hook = func() error { f.write("config.json", original+"\n"); return nil }
			case "unknown":
				_, err := ReviewRetirement(f.path("config.json"), "missing")
				if err == nil {
					t.Fatal("unknown accepted")
				}
				return
			case "link":
				must(t, os.Rename(f.path("config.json"), f.path("real.json")))
				must(t, os.Symlink("real.json", f.path("config.json")))
			}
			_, err = applyRetirement(f.path("config.json"), review.Observation, "rules", hook)
			if err == nil {
				t.Fatal("unsafe retirement accepted")
			}
			want := original
			if scenario == "stale" || scenario == "race" {
				want += "\n"
			}
			f.expect("config.json", want)
		})
	}
}

func TestRetirementInheritedOverrideAndCoordinator(t *testing.T) {
	f := newFixture(t)
	f.write("parent.json", f.read("config.json"))
	f.raw.Extends = "parent.json"
	f.raw.CoordinationDir = "coordinator"
	f.load()
	must(t, writeJSON(f.path("coordinator/profiles.json"), ownershipRoster{Version: 1, Profiles: []string{f.path("config.json")}}))
	f.apply()
	parent := f.read("parent.json")
	roster := f.read("coordinator/profiles.json")
	review, err := ReviewRetirement(f.path("config.json"), "rules")
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), review.Observation, "rules")
	must(t, err)
	current, err := LoadConfig(f.path("config.json"))
	must(t, err)
	if len(current.Resources) != 0 {
		t.Fatal("parent definition resurfaced")
	}
	_, err = Apply(current, Options{})
	must(t, err)
	f.expect("parent.json", parent)
	f.expect("coordinator/profiles.json", roster)
}

func TestRetirementAllAdaptersPreserveNativeContent(t *testing.T) {
	for name, fixtureFn := range map[string]func(*testing.T) *fixture{
		"raw": newFixture, "instructions": instructionsFixture,
		"agent": agentSettingsFixture, "hooks": hooksFixture,
		"mcp": mcpPolicyFixture, "skill-policy": invocationFixture,
		"plugin": pluginFixture, "plugin-agents": pluginAgentFixture,
	} {
		t.Run(name, func(t *testing.T) {
			f := fixtureFn(t)
			f.apply()
			before := auditTree(t, f.dir)
			id := f.c.Resources[0].ID
			review, err := ReviewRetirement(f.path("config.json"), id)
			must(t, err)
			_, err = ApplyRetirement(f.path("config.json"), review.Observation, id)
			must(t, err)
			after := auditTree(t, f.dir)
			for path, value := range before {
				if path != f.path("config.json") && value != after[path] {
					t.Fatalf("changed %s", path)
				}
			}
		})
	}
}
