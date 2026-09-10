package bridge

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func instructionSetFixture(t *testing.T, alternateOnly bool) *fixture {
	f := conventionFixture(t)
	if alternateOnly {
		must(t, os.Remove(f.path("CLAUDE.md")))
	} else {
		f.write("CLAUDE.md", "root\r\nwithout trailing newline")
	}
	f.write(".claude/CLAUDE.md", "alternate\n")
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 || f.c.Resources[0].Kind != "instruction-set" {
		t.Fatal("not discovered as an instruction set", f.c.Resources)
	}
	return f
}

func TestInstructionSetRoundTrip(t *testing.T) {
	for _, alternateOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "both", true: "alternate-only"}[alternateOnly], func(t *testing.T) {
			f := instructionSetFixture(t, alternateOnly)
			original, err := readItemSide(Item{Resource: f.c.Resources[0], Adapter: "instruction-set"}, "claude")
			must(t, err)
			f.apply()
			current, err := readItemSide(Item{Resource: f.c.Resources[0], Adapter: "instruction-set"}, "claude")
			must(t, err)
			if !equal(original, current) {
				t.Fatal("first sync changed source bytes/modes")
			}
			p, err := Plan(f.c)
			must(t, err)
			if len(p.Items[0].Writes) != 0 {
				t.Fatal("not converged")
			}
			f.write("AGENTS.md", strings.Replace(f.read("AGENTS.md"), "alternate\n", "edited alternate\n", 1))
			f.apply()
			f.expect(".claude/CLAUDE.md", "edited alternate\n")
			if alternateOnly {
				f.missing("CLAUDE.md")
			} else {
				f.expect("CLAUDE.md", "root\r\nwithout trailing newline")
			}
			f.write(".claude/CLAUDE.md", "forward\r\n")
			f.apply()
			if !strings.Contains(f.read("AGENTS.md"), "forward\r\n") {
				t.Fatal("forward update lost")
			}
			before := auditTree(t, f.dir)
			f.apply()
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("idempotent sync wrote")
			}
		})
	}
}

func TestInstructionSetRefusesLoss(t *testing.T) {
	for _, mode := range []string{"source-delete", "section-delete", "unmarked", "reordered", "duplicate", "import", "reserved", "concurrent", "symlink", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			f := instructionSetFixture(t, false)
			f.apply()
			codex := f.read("AGENTS.md")
			switch mode {
			case "source-delete":
				must(t, os.Remove(f.path(".claude/CLAUDE.md")))
			case "section-delete":
				f.write("AGENTS.md", codex[:strings.Index(codex, instructionSourceMarker+"alternate:start")])
			case "unmarked":
				f.write("AGENTS.md", codex+"unmapped instructions")
			case "reordered":
				pos := strings.Index(codex, instructionSourceMarker+"alternate:start")
				f.write("AGENTS.md", codex[pos:]+codex[:pos])
			case "duplicate":
				f.write("AGENTS.md", codex+codex)
			case "import":
				f.write(".claude/CLAUDE.md", "Read @secrets.md")
			case "reserved":
				f.write(".claude/CLAUDE.md", instructionSourceMarker+"root:start -->")
			case "concurrent":
				f.write(".claude/CLAUDE.md", "Claude change")
				f.write("AGENTS.md", strings.Replace(codex, "alternate\n", "different\n", 1))
			case "symlink", "hardlink":
				must(t, os.Remove(f.path(".claude/CLAUDE.md")))
				if mode == "symlink" {
					must(t, os.Symlink(f.path("CLAUDE.md"), f.path(".claude/CLAUDE.md")))
				} else {
					must(t, os.Link(f.path("CLAUDE.md"), f.path(".claude/CLAUDE.md")))
				}
			}
			before := auditTree(t, f.dir)
			_, err := Apply(f.c, Options{})
			if err == nil {
				t.Fatal("unsafe change accepted")
			}
			if mode == "concurrent" && !errors.Is(err, ErrConflicts) {
				t.Fatal("divergent edits did not produce a conflict", err)
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("failed validation changed files")
			}
		})
	}
}

func TestInstructionSetRollbackHistoryAndObservation(t *testing.T) {
	for _, alternateOnly := range []bool{false, true} {
		f := instructionSetFixture(t, alternateOnly)
		f.apply()
		codex := f.read("AGENTS.md")
		f.write("AGENTS.md", strings.Replace(codex, "alternate\n", "new alternate\n", 1))
		_, err := Apply(f.c, Options{BeforeWrite: func(n int, op Operation) error {
			if strings.HasSuffix(op.Label, "-claude-alternate") {
				return errors.New("injected")
			}
			return nil
		}})
		contains(t, err, "injected")
		f.expect(".claude/CLAUDE.md", "alternate\n")
		f.missing("state/pending.json")
		f.apply()
		h, err := History(f.path("config.json"))
		must(t, err)
		var choice HistoryChoice
		for _, entry := range h {
			j, _, err := readHistoryJournal(f.c, entry.Transaction)
			must(t, err)
			for _, op := range j.Operations {
				if strings.HasSuffix(op.Label, "-claude-alternate") && stringSnapshot(op.After) == "new alternate\n" {
					choice = HistoryChoice{entry.Transaction, f.c.Resources[0].ID, "claude", "after"}
				}
			}
		}
		if choice.Transaction == "" {
			t.Fatal("complete Claude history missing")
		}
		f.write(".claude/CLAUDE.md", "latest\n")
		f.apply()
		review, err := ReviewHistory(f.path("config.json"), choice)
		must(t, err)
		_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
		must(t, err)
		f.expect(".claude/CLAUDE.md", "new alternate\n")
		p, err := Plan(f.c)
		must(t, err)
		f.write(".claude/CLAUDE.md", "later edit\n")
		_, err = Apply(f.c, Options{ExpectedObservation: Observation(f.c, p)})
		if !errors.Is(err, ErrObservationChanged) {
			t.Fatal(err)
		}
	}
}

func stringSnapshot(s *Snapshot) string { b, _ := snapshotBytes(s); return string(b) }

func FuzzInstructionSetRoundTrip(f *testing.F) {
	f.Add("root\r\n", "alternate without newline", true)
	f.Add("", "", false)
	f.Fuzz(func(t *testing.T, root, alternate string, hasRoot bool) {
		s := instructionSet{Alternate: &alternate}
		if hasRoot {
			s.Root = &root
		}
		if validateInstructionSet(s) != nil {
			return
		}
		canonical, err := encoded(s)
		must(t, err)
		for _, side := range sides {
			rendered, err := renderInstructionSet(side, canonical, nil)
			must(t, err)
			normalized, err := normalizeInstructionSet(side, rendered)
			must(t, err)
			if !equal(canonical, normalized) {
				t.Fatal("instruction bytes did not round-trip")
			}
		}
	})
}

func TestInstructionSetSourceAdditionAndModes(t *testing.T) {
	f := instructionSetFixture(t, true)
	must(t, os.Chmod(f.path(".claude/CLAUDE.md"), 0750))
	f.apply()
	f.write("CLAUDE.md", "new root")
	must(t, os.Chmod(f.path("CLAUDE.md"), 0640))
	reloadConventions(t, f)
	f.apply()
	f.write("AGENTS.md", strings.Replace(f.read("AGENTS.md"), "new root", "reverse root", 1))
	f.apply()
	f.expect("CLAUDE.md", "reverse root")
	root, err := snapshot(f.path("CLAUDE.md"))
	must(t, err)
	alt, err := snapshot(f.path(".claude/CLAUDE.md"))
	must(t, err)
	if root.Mode != 0640 || alt.Mode != 0750 {
		t.Fatal("native permissions changed", root.Mode, alt.Mode)
	}
}

func TestInstructionSetFootprintAndLayoutMigration(t *testing.T) {
	f := instructionSetFixture(t, false)
	r := f.c.Resources[0]
	companion := f.path(".claude/CLAUDE.md")
	if !allowedTarget(f.c, companion) || allowedTarget(f.c, f.path(".claude/unmanaged.md")) {
		t.Fatal("incorrect recovery footprint")
	}
	if !hasField(resourceDestinations(r), companion) {
		t.Fatal("ownership footprint omits companion")
	}
	// Explicit profiles protect the derived companion even without conventions.
	f.raw.Conventions = nil
	f.raw.Resources = []resourceInput{{ID: "set", Kind: "instruction-set", Scope: "project", Portable: true, Claude: "CLAUDE.md", Codex: "AGENTS.md"}}
	f.load()
	paths, err := DiagnosticProtectedPaths(f.path("config.json"))
	must(t, err)
	if !hasField(paths, companion) {
		t.Fatal("logs can overlap instruction companion")
	}
	f.raw.Resources = append(f.raw.Resources, resourceInput{ID: "collision", Kind: "portable-file", Scope: "project", Claude: ".claude/CLAUDE.md", Codex: "other.md"})
	f.save()
	_, err = LoadConfig(f.path("config.json"))
	contains(t, err, "overlap")
	g := conventionFixture(t)
	reloadConventions(t, g)
	g.apply()
	g.write(".claude/CLAUDE.md", "new source")
	_, err = LoadConfig(g.path("config.json"))
	contains(t, err, "layout changed")
}

func TestInstructionSetRetainsMissingCompanionForRecovery(t *testing.T) {
	f := instructionSetFixture(t, false)
	f.apply()
	must(t, os.Remove(f.path(".claude/CLAUDE.md")))
	reloadConventions(t, f)
	if f.c.Resources[0].Kind != "instruction-set" {
		t.Fatal("missing source changed recovery identity")
	}
	_, err := Plan(f.c)
	contains(t, err, "source deletion")
	must(t, os.Remove(f.path("CLAUDE.md")))
	must(t, os.Remove(f.path("AGENTS.md")))
	reloadConventions(t, f)
	if f.c.Resources[0].Kind != "instruction-set" {
		t.Fatal("lost set identity")
	}
	p, err := Plan(f.c)
	must(t, err)
	if !p.HasConflicts() {
		t.Fatal("whole-set deletion not reported")
	}
}

func TestInstructionSetExclusionBeforeCompanionRead(t *testing.T) {
	f := conventionFixture(t)
	f.write("nested/CLAUDE.md", "excluded")
	must(t, os.Symlink(f.dir, f.path("nested/.claude")))
	f.write("config.json", `{"version":1,"stateDir":"state","resources":[],"conventions":{"root":".","features":["instructions"],"exclude":["nested/CLAUDE.md"]}}`)
	reloadConventions(t, f)
	f.apply()
	f.missing("nested/AGENTS.md")
}
