package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func conventionFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":"."},"resources":[]}`)
	return f
}

func TestConventionsUnsafeAndSpecialCases(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "case", "override", "import", "invalid-root", "invalid-exclusion", "unknown-option"} {
		t.Run(kind, func(t *testing.T) {
			f := conventionFixture(t)
			switch kind {
			case "symlink":
				must(t, os.Symlink(f.path("CLAUDE.md"), f.path("AGENTS.md")))
			case "hardlink":
				must(t, os.Link(f.path("CLAUDE.md"), f.path("AGENTS.md")))
			case "case":
				f.write("src/claude.md", "rules")
			case "override":
				f.write("AGENTS.override.md", "override")
			case "import":
				f.write("CLAUDE.md", "Read @docs/rules.md for rules")
			case "invalid-root":
				f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":"/"},"resources":[]}`)
			case "invalid-exclusion":
				f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","exclude":["../outside"]},"resources":[]}`)
			case "unknown-option":
				f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","typo":true},"resources":[]}`)
			}
			c, err := LoadAuditConfig(f.path("config.json"))
			if kind == "unknown-option" {
				if _, loadErr := LoadConfig(f.path("config.json")); loadErr == nil {
					t.Fatal("sync loader silently ignored unsafe typo")
				}
			}
			if err == nil {
				_, err = Plan(c)
			}
			if err == nil {
				t.Fatal("unsafe/unsupported convention accepted")
			}
			f.missing("state/manifest.json")
		})
	}
}

func TestConventionsReadOnlyWarningsAndExplicitMigration(t *testing.T) {
	f := conventionFixture(t)
	f.write("CLAUDE.local.md", "PRIVATE_SECRET")
	f.write("src/CLAUDE.md", "nested")
	// Existing explicit root keeps its identity and baseline.
	f.load()
	f.apply()
	f.raw.Conventions = &Conventions{Root: "."}
	f.load()
	if len(f.c.Resources) != 2 || f.c.Resources[0].ID != "rules" {
		t.Fatal("explicit identity replaced")
	}
	if len(f.c.ConventionWarnings) != 1 || strings.Contains(f.c.ConventionWarnings[0], "PRIVATE_SECRET") {
		t.Fatal("missing or private warning")
	}
	before := f.read("config.json")
	review, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.expect("config.json", before)
	f.missing("src/AGENTS.md")
	_, err = SyncReviewed(f.path("config.json"), review.Observation)
	must(t, err)
	f.expect("src/AGENTS.md", "nested")
	if len(Audit(f.c).ConventionWarnings) != 1 {
		t.Fatal("audit omitted warning")
	}
}

func TestConventionsFreshnessAndRollback(t *testing.T) {
	f := conventionFixture(t)
	reloadConventions(t, f)
	review, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("new/CLAUDE.md", "new")
	_, err = Apply(f.c, Options{})
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatalf("stale inventory accepted: %v", err)
	}
	_, err = SyncReviewed(f.path("config.json"), review.Observation)
	if !errors.Is(err, ErrObservationChanged) {
		t.Fatalf("stale review accepted: %v", err)
	}
	f.missing("AGENTS.md")
	reloadConventions(t, f)
	_, err = Apply(f.c, Options{BeforeWrite: func(index int, op Operation) error {
		if index == 1 {
			return errors.New("fixture write failure")
		}
		return nil
	}})
	contains(t, err, "rolled back")
	f.missing("AGENTS.md")
	f.missing("new/AGENTS.md")
	f.expect("new/CLAUDE.md", "new")
	f.apply()
}

func TestConventionsRetirementAndFlattening(t *testing.T) {
	f := conventionFixture(t)
	f.write("src/CLAUDE.md", "nested")
	reloadConventions(t, f)
	f.apply()
	flat, err := flatProfile(f.c)
	must(t, err)
	var raw configInput
	must(t, decodeEnrollmentJSON(flat, &raw))
	if raw.Conventions == nil || len(raw.Resources) != 0 {
		t.Fatal("flattening lost policy or froze discovered pairs")
	}
	id := conventionEntry(f.dir, f.path("src")).ID
	r, err := ReviewRetirement(f.path("config.json"), id)
	must(t, err)
	_, err = ApplyRetirement(f.path("config.json"), r.Observation, id)
	must(t, err)
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 {
		t.Fatal("retired pair was rediscovered")
	}
	f.expect("src/CLAUDE.md", "nested")
	f.expect("src/AGENTS.md", "nested")
	f.apply()
}

func TestConventionsTrackedBoundariesAndForgedManifest(t *testing.T) {
	for _, kind := range []string{"nested-repo", "forged-path", "root-change"} {
		t.Run(kind, func(t *testing.T) {
			f := conventionFixture(t)
			f.write("src/CLAUDE.md", "rules")
			reloadConventions(t, f)
			f.apply()
			switch kind {
			case "nested-repo":
				f.write("src/.git", "gitdir: external")
			case "forged-path":
				m, _, err := readManifest(f.c)
				must(t, err)
				id := conventionEntry(f.dir, f.path("src")).ID
				r := m.Resources[id]
				r.Paths["codex"] = f.path("unrelated.txt")
				m.Resources[id] = r
				must(t, writeJSON(manifestPath(f.c), m))
			case "root-change":
				f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":"src"},"resources":[]}`)
			}
			if _, err := LoadConfig(f.path("config.json")); err == nil {
				t.Fatal("unsafe tracked change accepted")
			}
		})
	}
}

func TestConventionsPendingFirstSyncRetainsMissingPair(t *testing.T) {
	f := conventionFixture(t)
	reloadConventions(t, f)
	p := f.plan()
	r := f.c.Resources[0]
	// Construct an interrupted, not-yet-written legacy transaction. Recovery
	// must retain its authorized targets even if the sole native source vanished.
	m := Manifest{Version: 2, Resources: map[string]Resource{r.ID: r}, Files: map[string]string{r.ID: p.Items[0].Digest}}
	after, err := encoded(m)
	must(t, err)
	tx := "11111111-1111-4111-8111-111111111111"
	must(t, writeJSON(filepath.Join(f.c.StateDir, "backups", tx, "journal.json"), Journal{Version: 1, Operations: []Operation{
		{Label: r.ID + "-codex", File: r.Paths["codex"], After: p.Items[0].Content},
		{Label: "manifest", File: manifestPath(f.c), After: after},
	}}))
	must(t, writeJSON(pendingPath(f.c), Pending{Transaction: tx}))
	must(t, os.Remove(f.path("CLAUDE.md")))
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 {
		t.Fatal("pending identity disappeared")
	}
	_, err = Recover(f.c)
	must(t, err)
	f.missing("AGENTS.md")
	f.missing("state/pending.json")
}

func TestConventionsExclusionsOverrideAndInheritance(t *testing.T) {
	f := conventionFixture(t)
	f.write("AGENTS.override.md", "host only")
	f.write(".claude/CLAUDE.md", "alternate")
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","exclude":[".claude","AGENTS.override.md"]},"resources":[]}`)
	reloadConventions(t, f)
	f.apply()
	f.expect("AGENTS.override.md", "host only")
	f.expect(".claude/CLAUDE.md", "alternate")
	f.write("child.json", `{"version":1,"stateDir":"child-state","extends":"config.json","resources":[]}`)
	_, err := LoadConfig(f.path("child.json"))
	contains(t, err, "leaf profile")
}

func TestConventionsReservedNamespaceAndPartialOverlap(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		f := newFixture(t)
		f.raw.Conventions = &Conventions{Root: "."}
		if reserved {
			f.raw.Resources[0].ID = conventionPrefix + "custom"
		} else {
			f.raw.Resources[0].Codex = "other.md"
		}
		f.save()
		if _, err := LoadConfig(f.path("config.json")); err == nil {
			t.Fatal("ambiguous ownership accepted")
		}
	}
}

func TestConventionsReviewedEnrollmentKeepsDynamicPolicy(t *testing.T) {
	f := conventionFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","coordinationDir":"coordination","conventions":{"root":"."},"resources":[]}`)
	target := f.path("registered.json")
	r, err := ReviewEnrollmentCreation(f.path("config.json"), target)
	must(t, err)
	must(t, CreateEnrolledReviewed(f.path("config.json"), target, r.Observation))
	f.write("new/CLAUDE.md", "new")
	c, err := LoadAuditConfig(target)
	must(t, err)
	if c.Conventions == nil || len(c.Resources) != 2 {
		t.Fatal("enrollment froze convention inventory")
	}
	_, err = Apply(c, Options{})
	must(t, err)
	f.expect("new/AGENTS.md", "new")
}

func reloadConventions(t *testing.T, f *fixture) {
	t.Helper()
	var err error
	f.c, err = LoadConfig(f.path("config.json"))
	must(t, err)
}

func TestConventionsNestedBidirectionalAndNewFiles(t *testing.T) {
	f := conventionFixture(t)
	f.write("src/api/CLAUDE.md", "api rules")
	f.write("docs/AGENTS.md", "docs rules")
	reloadConventions(t, f)
	if len(f.c.Resources) != 3 {
		t.Fatalf("discovered %d pairs, want 3", len(f.c.Resources))
	}
	f.apply()
	f.expect("src/api/AGENTS.md", "api rules")
	f.expect("docs/CLAUDE.md", "docs rules")
	f.write("src/api/AGENTS.md", "updated api")
	f.write("new/deep/CLAUDE.md", "new rules")
	reloadConventions(t, f)
	f.apply()
	f.expect("src/api/CLAUDE.md", "updated api")
	f.expect("new/deep/AGENTS.md", "new rules")
	reloadConventions(t, f)
	for _, item := range f.plan().Items {
		if len(item.Writes) != 0 {
			t.Fatal("sync is not idempotent")
		}
	}
}

func TestConventionsConflictsAndDeletedPair(t *testing.T) {
	f := conventionFixture(t)
	f.write("src/CLAUDE.md", "left")
	f.write("src/AGENTS.md", "right")
	reloadConventions(t, f)
	if !f.plan().HasConflicts() {
		t.Fatal("initial conflict not detected")
	}
	_, err := Apply(f.c, Options{})
	if err == nil {
		t.Fatal("conflict accepted")
	}
	f.missing("AGENTS.md")
	f.write("src/AGENTS.md", "left")
	reloadConventions(t, f)
	f.apply()
	must(t, os.Remove(f.path("src/CLAUDE.md")))
	must(t, os.Remove(f.path("src/AGENTS.md")))
	reloadConventions(t, f)
	if len(f.c.Resources) != 2 || !f.plan().HasConflicts() {
		t.Fatal("deleted pair was forgotten")
	}
}

func TestConventionsSkipBoundaries(t *testing.T) {
	f := conventionFixture(t)
	for _, dir := range []string{"node_modules/pkg", "vendor/pkg", "dist", ".git", ".hidden", "state/shared", "STATE/private", "excluded", "separate"} {
		f.write(dir+"/CLAUDE.md", "not adopted")
	}
	f.write("separate/.git", "gitdir: elsewhere")
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","exclude":["excluded"]},"resources":[]}`)
	must(t, os.Symlink(f.path("excluded"), f.path("linked")))
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 {
		t.Fatalf("escaped boundaries: %+v", f.c.Resources)
	}
	f.apply()
	f.missing("excluded/AGENTS.md")
	f.missing("separate/AGENTS.md")
}
