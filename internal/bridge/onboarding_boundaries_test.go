package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConventionInitializationOnlyCreatesPrivateProfile(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			root := onboardingRoot(t)
			profile := filepath.Join(root, "profile.json")
			var err error
			if scope == "project" {
				err = InitConventionProfile(profile)
			} else {
				err = InitGlobalConventionProfile(profile, root)
			}
			must(t, err)
			data, err := os.ReadFile(profile)
			must(t, err)
			var raw configInput
			must(t, json.Unmarshal(data, &raw))
			if raw.Version != 1 || raw.StateDir != ".agent-bridge" || len(raw.Resources) != 0 || raw.Conventions == nil || raw.Conventions.Scope != scope {
				t.Fatal("wrong initialized policy")
			}
			if !reflect.DeepEqual(raw.Conventions.Features, []string{"instructions", "skills", "agents", "hooks", "mcp", "plugins"}) {
				t.Fatal("initialization did not select all supported categories")
			}
			c, err := LoadConfig(profile)
			must(t, err)
			if c.Conventions.Root != root {
				t.Fatal("initialized policy changed the selected root")
			}
			entries, err := os.ReadDir(root)
			must(t, err)
			if len(entries) != 1 || entries[0].Name() != "profile.json" {
				t.Fatal("initialization created native assets or synchronization state")
			}
			info, err := os.Stat(profile)
			must(t, err)
			if info.Mode().Perm() != 0600 {
				t.Fatal("profile is not private")
			}
		})
	}
}

func TestGlobalInitializationRejectsUnsafeRootsWithoutCreatingProfile(t *testing.T) {
	for _, kind := range []string{"empty", "tilde", "missing", "file", "link", "filesystem-root"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			root := ""
			switch kind {
			case "tilde":
				root = "~/not-expanded"
			case "missing":
				root = f.path("missing")
			case "file":
				f.write("file", "preserve")
				root = f.path("file")
			case "link":
				must(t, os.Symlink(f.dir, f.path("alias")))
				root = f.path("alias")
			case "filesystem-root":
				root = string(filepath.Separator)
			}
			before := auditTree(t, f.dir)
			if err := InitGlobalConventionProfile(f.path("new.json"), root); err == nil {
				t.Fatal("unsafe global root accepted")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("rejected initialization wrote files")
			}
		})
	}
}
