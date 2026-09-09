package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func creationFixture(t *testing.T) *fixture {
	f := newFixture(t)
	f.raw.CoordinationDir = "coordination"
	f.load()
	return f
}

func TestNewProfileEnrollment(t *testing.T) {
	f := creationFixture(t)
	target := f.path("registered.json")
	before := auditTree(t, f.dir)
	r, err := ReviewEnrollmentCreation(f.path("config.json"), target)
	must(t, err)
	if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
		t.Fatal("review wrote")
	}
	must(t, CreateEnrolledReviewed(f.path("config.json"), target, r.Observation))
	f.missing("AGENTS.md")
	f.missing("state")
	f.missing("coordination/enrollment-pending.json")
	c, err := LoadAuditConfig(target)
	must(t, err)
	if c.Resources[0].Paths["claude"] != f.path("CLAUDE.md") {
		t.Fatal("relative source changed")
	}
	_, err = Apply(c, Options{})
	must(t, err)
	f.expect("AGENTS.md", "one")
	info, err := os.Stat(target)
	must(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatal("profile not private")
	}
	if err := CreateEnrolledReviewed(f.path("config.json"), target, r.Observation); err == nil {
		t.Fatal("overwrote profile")
	}
}

func TestNewEnrollmentStaleAndRollback(t *testing.T) {
	for _, mode := range []string{"stale-source", "stale-roster", "profile-failure", "roster-failure", "concurrent-profile", "later-profile", "crash"} {
		t.Run(mode, func(t *testing.T) {
			f := creationFixture(t)
			target := f.path("registered.json")
			r, err := ReviewEnrollmentCreation(f.path("config.json"), target)
			must(t, err)
			if mode == "stale-source" {
				f.write("CLAUDE.md", "changed")
			}
			if mode == "stale-roster" {
				f.write("coordination/profiles.json", "{}")
			}
			var hook func(string) error
			switch mode {
			case "profile-failure":
				hook = func(stage string) error { return errors.New("injected") }
			case "roster-failure":
				hook = func(stage string) error {
					if stage == "roster" {
						return errors.New("injected")
					}
					return nil
				}
			case "concurrent-profile":
				hook = func(stage string) error {
					if stage == "profile" {
						f.write("registered.json", "external")
					}
					return nil
				}
			case "later-profile":
				hook = func(stage string) error {
					if stage == "roster" {
						f.write("registered.json", "later edit")
						return errors.New("injected")
					}
					return nil
				}
			case "crash":
				hook = func(stage string) error {
					if stage == "roster" {
						panic("simulated interruption")
					}
					return nil
				}
			}
			if mode == "crash" {
				func() {
					defer func() {
						if recover() == nil {
							t.Fatal("no interruption")
						}
					}()
					_ = createEnrolledReviewed(f.path("config.json"), target, r.Observation, hook)
				}()
				c, err := LoadAuditConfig(target)
				must(t, err)
				_, err = Apply(c, Options{})
				contains(t, err, "enrollment")
				must(t, RecoverEnrollmentCreation(target, f.c.CoordinationDir))
			} else {
				err = createEnrolledReviewed(f.path("config.json"), target, r.Observation, hook)
				if err == nil {
					t.Fatal("expected rejected enrollment")
				}
			}
			if mode == "later-profile" {
				f.expect("registered.json", "later edit")
				if err := RecoverEnrollmentCreation(target, f.c.CoordinationDir); err == nil {
					t.Fatal("overwrote later edit")
				}
			} else if mode == "concurrent-profile" {
				f.expect("registered.json", "external")
			} else {
				f.missing("registered.json")
			}
			f.missing("AGENTS.md")
		})
	}
}

func TestNewEnrollmentOwnershipAndIdentity(t *testing.T) {
	f := creationFixture(t)
	// A template already enrolled against these paths cannot be cloned as a
	// second owner, even though the new profile name is different.
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	must(t, EnrollReviewed(f.path("config.json"), r.Observation))
	if _, err := ReviewEnrollmentCreation(f.path("config.json"), f.path("registered.json")); err == nil {
		t.Fatal("duplicate ownership accepted")
	}
	for _, target := range []string{f.path("CLAUDE.md"), f.path("state/registered.json"), f.path("config.json"), "relative.json"} {
		if _, err := ReviewEnrollmentCreation(f.path("config.json"), target); err == nil {
			t.Fatal("unsafe target accepted", target)
		}
	}
	g := agentSettingsFixture(t)
	g.raw.CoordinationDir = "coordination"
	g.load()
	r, err = ReviewEnrollmentCreation(g.path("config.json"), g.path("registered.json"))
	must(t, err)
	must(t, CreateEnrolledReviewed(g.path("config.json"), g.path("registered.json"), r.Observation))
	c, err := LoadAuditConfig(g.path("registered.json"))
	must(t, err)
	if !c.Resources[0].PreserveAgentSettings {
		t.Fatal("lost consent")
	}
}

func TestEnrollmentRecoveryAmbiguousCreation(t *testing.T) {
	f := creationFixture(t)
	target := f.path("registered.json")
	p, err := prepareEnrollmentCreation(f.path("config.json"), target)
	must(t, err)
	id, err := uuid()
	must(t, err)
	must(t, writeJSON(filepath.Join(f.c.CoordinationDir, "enrollment-backups", id, "create.json"), p.Journal))
	must(t, writeJSON(enrollmentPendingPath(f.c.CoordinationDir), Pending{id}))
	must(t, writeSnapshot(target, p.Journal.Profile))
	if err := RecoverEnrollmentCreation(target, f.c.CoordinationDir); err == nil {
		t.Fatal("deleted ambiguously created profile")
	}
}

func TestNewEnrollmentAppendsExistingRoster(t *testing.T) {
	f := creationFixture(t)
	r, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	must(t, EnrollReviewed(f.path("config.json"), r.Observation))
	f.raw.StateDir = "second-state"
	f.raw.Resources[0].Claude = "second-claude"
	f.raw.Resources[0].Codex = "second-codex"
	data, err := encoded(f.raw)
	must(t, err)
	must(t, writeSnapshot(f.path("template-two.json"), data))
	f.write("second-claude", "second")
	r, err = ReviewEnrollmentCreation(f.path("template-two.json"), f.path("registered-two.json"))
	must(t, err)
	must(t, CreateEnrolledReviewed(f.path("template-two.json"), f.path("registered-two.json"), r.Observation))
	report, err := CheckOverlaps([]string{f.path("config.json"), f.path("registered-two.json")})
	must(t, err)
	if len(report.Overlaps) != 0 {
		t.Fatal(report)
	}
	c, err := LoadAuditConfig(f.path("registered-two.json"))
	must(t, err)
	_, err = Apply(c, Options{})
	must(t, err)
	f.expect("second-codex", "second")
}
