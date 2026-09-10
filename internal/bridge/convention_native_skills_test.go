package bridge

import (
	"reflect"
	"testing"
)

func TestGlobalConventionsProtectNativeSkills(t *testing.T) {
	for _, foreign := range []string{".codex/skills/.system/demo", ".codex/skills/demo", ".claude/plugins/cache/vendor/pkg/1/skills/demo", ".codex/plugins/cache/vendor/pkg/1/skills/v1/demo"} {
		t.Run(foreign, func(t *testing.T) {
			f := newFixture(t)
			f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"],"protectNativeSkills":true},"resources":[]}`)
			body := "---\nname: demo\ndescription: Demo.\n---\nUse the local fixture.\n"
			f.write(".claude/skills/demo/SKILL.md", body)
			reloadConventions(t, f)
			f.apply()
			// A native installation appears after the initial enrollment. The next
			// watcher/config load must block instead of creating competing copies.
			f.write(foreign+"/SKILL.md", body)
			before := auditTree(t, f.dir)
			if _, err := LoadConfig(f.path("config.json")); err == nil {
				t.Fatal("native collision was not blocked")
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("collision changed files")
			}
		})
	}
}

func TestNativeSkillProtectionMatchesMetadataAndHonorsExclusions(t *testing.T) {
	f := newFixture(t)
	body := "---\nname: declared-name\ndescription: Demo.\n---\nFixture.\n"
	f.write(".claude/skills/local-alias/SKILL.md", body)
	f.write(".codex/skills/.system/native-alias/SKILL.md", body)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"],"protectNativeSkills":true},"resources":[]}`)
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("metadata alias collision accepted")
	}
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"],"protectNativeSkills":true,"exclude":[".claude/skills/local-alias"]},"resources":[]}`)
	c, err := LoadConfig(f.path("config.json"))
	if err != nil || len(c.Resources) != 0 {
		t.Fatalf("reviewed exclusion failed: %v", err)
	}
	// Existing profiles keep their behavior unless they explicitly opt in.
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"]},"resources":[]}`)
	c, err = LoadConfig(f.path("config.json"))
	if err != nil || len(c.Resources) != 1 {
		t.Fatalf("legacy profile changed: %v", err)
	}
}

func TestNativeSkillProtectionRequiresGlobalSkills(t *testing.T) {
	for _, scope := range []string{"project", ""} {
		p := Conventions{Root: ".", Scope: scope, Features: []string{"skills"}, ProtectNativeSkills: true}
		if err := p.resolve(t.TempDir()); err == nil {
			t.Fatal("accepted project native guard")
		}
	}
	p := Conventions{Root: ".", Scope: "global", Features: []string{"instructions"}, ProtectNativeSkills: true}
	if err := p.resolve(t.TempDir()); err == nil {
		t.Fatal("accepted guard without skills")
	}
}
