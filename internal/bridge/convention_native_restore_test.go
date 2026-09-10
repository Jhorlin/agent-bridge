package bridge

import (
	"reflect"
	"strings"
	"testing"
)

func nativeGuardRenameFixture(t *testing.T) (*fixture, string, string) {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"],"protectNativeSkills":true},"resources":[]}`)
	body := "---\nname: old-name\ndescription: Demo.\n---\nFixture.\n"
	f.write(".claude/skills/alias/SKILL.md", body)
	reloadConventions(t, f)
	id := f.c.Resources[0].ID
	tx := initialHistory(t, f)
	f.write(".claude/skills/alias/SKILL.md", strings.Replace(body, "old-name", "new-name", 1))
	reloadConventions(t, f)
	f.apply()
	return f, id, tx
}

func TestNativeSkillGuardRejectsHistoricalNameCollision(t *testing.T) {
	f, id, tx := nativeGuardRenameFixture(t)
	choice := HistoryChoice{tx, id + "/SKILL.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	f.write(".codex/skills/.system/native-alias/SKILL.md", "---\nname: old-name\ndescription: Native.\n---\nNative.\n")
	before := auditTree(t, f.dir)
	if _, err := RestoreReviewed(f.path("config.json"), review.Observation, choice); err == nil {
		t.Fatal("history created a native skill collision")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("blocked history changed native files or journal")
	}
	if _, err := ReviewHistory(f.path("config.json"), choice); err == nil {
		t.Fatal("review accepted a prospective name collision")
	}
}

func TestNativeSkillGuardAllowsNonCollidingHistory(t *testing.T) {
	f, id, tx := nativeGuardRenameFixture(t)
	choice := HistoryChoice{tx, id + "/SKILL.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	for _, path := range []string{".claude/skills/alias/SKILL.md", ".agents/skills/alias/SKILL.md"} {
		if !strings.Contains(f.read(path), "name: old-name") {
			t.Fatal("safe historical name was not restored")
		}
	}
	reloadConventions(t, f)
	for _, s := range f.plan().Summaries() {
		if s.Status != "in-sync" {
			t.Fatal("safe history left pending changes")
		}
	}
}

func TestNativeSkillGuardRejectsSharedNameCollision(t *testing.T) {
	f, id, _ := nativeGuardRenameFixture(t)
	f.write(".codex/skills/.system/native-alias/SKILL.md", "---\nname: old-name\ndescription: Native.\n---\nNative.\n")
	shared := "state/shared/" + id + "/SKILL.md"
	f.write(shared, strings.Replace(f.read(shared), "name: new-name", "name: old-name", 1))
	reloadConventions(t, f)
	before := auditTree(t, f.dir)
	if _, err := Plan(f.c); err == nil {
		t.Fatal("plan accepted a canonical-source name collision")
	}
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("shared source created a native skill collision")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("blocked canonical input changed native files or journal")
	}
}

func TestNativeSkillGuardRejectsResolvedNameCollision(t *testing.T) {
	f, id, _ := nativeGuardRenameFixture(t)
	shared := "state/shared/" + id + "/SKILL.md"
	f.write(shared, strings.Replace(f.read(shared), "name: new-name", "name: old-name", 1))
	f.write(".claude/skills/alias/SKILL.md", strings.Replace(f.read(".claude/skills/alias/SKILL.md"), "Fixture.", "Different edit.", 1))
	choices := map[string]string{id + "/SKILL.md": "shared"}
	review, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	f.write(".codex/skills/.system/native-alias/SKILL.md", "---\nname: old-name\ndescription: Native.\n---\nNative.\n")
	before := auditTree(t, f.dir)
	if _, err := ResolveReviewed(f.path("config.json"), review.Observation, choices); err == nil {
		t.Fatal("resolution created a native name collision")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("blocked resolution changed native files or journal")
	}
	if _, err := ReviewResolution(f.path("config.json"), choices); err == nil {
		t.Fatal("review accepted a colliding resolution")
	}
}

func TestNativeSkillGuardRejectsTwoProspectiveNames(t *testing.T) {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["skills"],"protectNativeSkills":true},"resources":[]}`)
	for _, name := range []string{"first", "second"} {
		f.write(".claude/skills/"+name+"/SKILL.md", "---\nname: "+name+"\ndescription: Demo.\n---\nFixture.\n")
	}
	reloadConventions(t, f)
	f.apply()
	for _, r := range f.c.Resources {
		f.write("state/shared/"+r.ID+"/SKILL.md", "---\nname: same-future-name\ndescription: Demo.\n---\nFixture.\n")
	}
	before := auditTree(t, f.dir)
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("two canonical edits created duplicate names")
	}
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("prospective collision changed files")
	}
}
