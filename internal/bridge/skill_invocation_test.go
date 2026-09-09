package bridge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSkillInvocationEveryWriteRollback(t *testing.T) {
	for index := 0; index < 4; index++ {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			f := invocationFixture(t)
			f.apply()
			paths := []string{"state/shared/demo/SKILL.md", "codex-skill/SKILL.md", "codex-skill/agents/openai.yaml", "state/manifest.json"}
			before := map[string]*Snapshot{}
			for _, path := range paths {
				var err error
				before[path], err = snapshot(f.path(path))
				must(t, err)
			}
			f.write("claude-skill/SKILL.md", strings.ReplaceAll(strings.ReplaceAll(f.read("claude-skill/SKILL.md"), "true", "false"), "Exact body.", "New body."))
			_, err := Apply(f.c, Options{BeforeWrite: func(n int, op Operation) error {
				if n == index {
					if op.File != f.path(paths[index]) {
						t.Fatal("unexpected operation order", op.Label)
					}
					return errors.New("injected")
				}
				return nil
			}})
			contains(t, err, "rolled back")
			for path, old := range before {
				now, err := snapshot(f.path(path))
				must(t, err)
				if !equal(old, now) {
					t.Fatal("rollback changed bytes/mode", path)
				}
			}
			f.missing("state/pending.json")
			f.apply()
		})
	}
}

func TestSkillInvocationCrashHelper(t *testing.T) {
	filename := os.Getenv("AGENT_BRIDGE_SKILL_CRASH_CONFIG")
	if filename == "" {
		t.Skip("subprocess helper")
	}
	c, err := LoadConfig(filename)
	must(t, err)
	_, err = Apply(c, Options{BeforeWrite: func(_ int, op Operation) error {
		if strings.HasSuffix(op.Label, "-codex-policy") {
			os.Exit(91)
		}
		return nil
	}})
	t.Fatalf("helper did not crash: %v", err)
}

func TestSkillInvocationInterruptionAndLaterEdit(t *testing.T) {
	f := invocationFixture(t)
	f.apply()
	oldEntry, oldPolicy := f.read("codex-skill/SKILL.md"), f.read("codex-skill/agents/openai.yaml")
	f.write("claude-skill/SKILL.md", strings.ReplaceAll(strings.ReplaceAll(f.read("claude-skill/SKILL.md"), "true", "false"), "Exact body.", "New body."))
	cmd := exec.Command(os.Args[0], "-test.run=^TestSkillInvocationCrashHelper$")
	cmd.Env = append(os.Environ(), "AGENT_BRIDGE_SKILL_CRASH_CONFIG="+f.path("config.json"))
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 91 {
		t.Fatalf("unexpected helper exit: %v", err)
	}
	_, err = Recover(f.c)
	contains(t, err, "stale sync.lock")
	must(t, os.Remove(f.path("state/sync.lock")))
	f.write("codex-skill/agents/openai.yaml", oldPolicy+"# later edit\n")
	_, err = Recover(f.c)
	contains(t, err, "later edit")
	f.expect("codex-skill/agents/openai.yaml", oldPolicy+"# later edit\n")
	f.write("codex-skill/agents/openai.yaml", oldPolicy)
	r, err := Recover(f.c)
	must(t, err)
	if r.Status != "recovered" {
		t.Fatal(r)
	}
	f.expect("codex-skill/SKILL.md", oldEntry)
	f.expect("codex-skill/agents/openai.yaml", oldPolicy)
	f.apply()
}

func TestSkillInvocationSidecarBoundaries(t *testing.T) {
	for _, mutation := range []string{"missing", "symlink", "case", "claude"} {
		t.Run(mutation, func(t *testing.T) {
			f := invocationFixture(t)
			f.apply()
			old := f.read("state/manifest.json")
			switch mutation {
			case "missing":
				must(t, os.Remove(f.path("codex-skill/agents/openai.yaml")))
			case "symlink":
				must(t, os.Rename(f.path("codex-skill/agents/openai.yaml"), f.path("external-policy")))
				must(t, os.Symlink(f.path("external-policy"), f.path("codex-skill/agents/openai.yaml")))
			case "case":
				must(t, os.Rename(f.path("codex-skill/agents/openai.yaml"), f.path("codex-skill/agents/OpenAI.yaml")))
			case "claude":
				f.write("claude-skill/agents/openai.yaml", "policy:\n  allow_implicit_invocation: false\n")
			}
			if _, err := Apply(f.c, Options{}); err == nil {
				t.Fatal("unsafe sidecar accepted")
			}
			f.expect("state/manifest.json", old)
			f.missing("state/pending.json")
		})
	}
	f := strictSkillFixture(t)
	f.apply()
	f.raw.Resources[0].TranslateSkillInvocation = true
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("changed identity accepted")
	}
	f = invocationFixture(t)
	f.raw.Resources[0].AllowReformat = false
	must(t, writeJSON(f.path("config.json"), f.raw))
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("missing reformat consent accepted")
	}
}

func FuzzSkillInvocationRoundTrip(f *testing.F) {
	f.Add("---\nname: demo\ndescription: Demo.\ndisable-model-invocation: true\n---\nInstructions.\r\n")
	f.Add("---\nname: demo\ndescription: Demo.\n---\nInstructions.\n")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			return
		}
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0700}
		common, err := normalizeSkillInvocation("claude", raw)
		if err != nil {
			return
		}
		for _, side := range sides {
			rendered, err := renderSkillInvocation(side, common)
			must(t, err)
			again, err := normalizeSkillInvocation(side, rendered)
			must(t, err)
			if !equal(common, again) {
				t.Fatal("invocation metadata failed round-trip", side)
			}
		}
	})
}

func invocationFixture(t *testing.T) *fixture {
	f := skillFixture(t)
	f.raw.Resources[0].AllowReformat = true
	f.raw.Resources[0].TranslateSkillInvocation = true
	f.load()
	f.write("claude-skill/SKILL.md", "---\nname: demo\ndescription: Invocation fixture.\ndisable-model-invocation: true\n---\nExact body.\r\n")
	return f
}

func TestSkillInvocationTranslation(t *testing.T) {
	f := invocationFixture(t)
	if Audit(f.c).Blocked() {
		t.Fatal("valid invocation fixture failed audit")
	}
	f.apply()
	if strings.Contains(f.read("codex-skill/SKILL.md"), "disable-model-invocation") {
		t.Fatal("Claude policy leaked into Codex entry")
	}
	f.expect("codex-skill/agents/openai.yaml", "policy:\n  allow_implicit_invocation: false\n")
	if !strings.HasSuffix(f.read("codex-skill/SKILL.md"), "Exact body.\r\n") {
		t.Fatal("instruction bytes changed")
	}
	f.write("codex-skill/agents/openai.yaml", "policy:\n  allow_implicit_invocation: true\n")
	f.apply()
	if !strings.Contains(f.read("claude-skill/SKILL.md"), "disable-model-invocation: false") {
		t.Fatal("reverse policy not translated")
	}
	before := f.read("state/manifest.json")
	f.apply()
	f.expect("state/manifest.json", before)
	flat, err := flatProfile(f.c)
	must(t, err)
	must(t, writeSnapshot(f.path("flat.json"), flat))
	c, err := LoadConfig(f.path("flat.json"))
	must(t, err)
	if !c.Resources[0].TranslateSkillInvocation {
		t.Fatal("enrollment flattening lost invocation flag")
	}
}

func TestSkillInvocationHistoryRequiresCompanion(t *testing.T) {
	f := invocationFixture(t)
	tx := initialHistory(t, f)
	j, _, err := readHistoryJournal(f.c, tx)
	must(t, err)
	filtered := []Operation{}
	for _, op := range j.Operations {
		if !strings.HasSuffix(op.Label, "-codex-policy") {
			filtered = append(filtered, op)
		}
	}
	j.Operations = filtered
	must(t, writeJSON(f.path("state/backups/"+tx+"/journal.json"), j))
	_, err = ReviewHistory(f.path("config.json"), HistoryChoice{tx, "demo/SKILL.md", "codex", "after"})
	contains(t, err, "companion missing")
}

func TestSkillInvocationRollbackAndRawReview(t *testing.T) {
	f := invocationFixture(t)
	f.apply()
	oldEntry, oldPolicy := f.read("codex-skill/SKILL.md"), f.read("codex-skill/agents/openai.yaml")
	f.write("claude-skill/SKILL.md", strings.ReplaceAll(strings.ReplaceAll(f.read("claude-skill/SKILL.md"), "true", "false"), "Exact body.", "New body."))
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("codex-skill/SKILL.md", oldEntry)
	f.expect("codex-skill/agents/openai.yaml", oldPolicy)
	review, err := ReviewProfile(f.path("config.json"))
	must(t, err)
	f.write("codex-skill/agents/openai.yaml", oldPolicy+"# local formatting\n")
	_, err = SyncReviewed(f.path("config.json"), review.Observation)
	contains(t, err, "changed")
	f.apply()
	if !strings.Contains(f.read("codex-skill/agents/openai.yaml"), "true") {
		t.Fatal("policy not applied")
	}
}

func TestSkillInvocationHistoryAndConflict(t *testing.T) {
	f := invocationFixture(t)
	tx := initialHistory(t, f)
	f.write("claude-skill/SKILL.md", strings.ReplaceAll(f.read("claude-skill/SKILL.md"), "true", "false"))
	f.apply()
	choice := HistoryChoice{tx, "demo/SKILL.md", "codex", "after"}
	review, err := ReviewHistory(f.path("config.json"), choice)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), review.Observation, choice)
	must(t, err)
	if !strings.Contains(f.read("codex-skill/agents/openai.yaml"), "false") {
		t.Fatal("historical policy not restored")
	}
	f.write("claude-skill/SKILL.md", strings.ReplaceAll(f.read("claude-skill/SKILL.md"), "Exact body.", "Claude body."))
	f.write("codex-skill/SKILL.md", strings.ReplaceAll(f.read("codex-skill/SKILL.md"), "Exact body.", "Codex body."))
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
	choices := map[string]string{"demo/SKILL.md": "codex"}
	r, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read("claude-skill/SKILL.md"), "Codex body.") {
		t.Fatal("composite conflict not resolved")
	}
}

func TestSkillInvocationRefusals(t *testing.T) {
	for _, policy := range []string{"policy:\n  allow_implicit_invocation: no\n", "policy:\n  allow_implicit_invocation: true\n---\nextra: value\n", "interface:\n  display_name: local\n", "policy:\n  allow_implicit_invocation: true\n  other: false\n", "policy: &p\n  allow_implicit_invocation: true\n"} {
		f := invocationFixture(t)
		f.apply()
		f.write("codex-skill/agents/openai.yaml", policy)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsupported policy accepted", policy)
		}
	}
	f := invocationFixture(t)
	f.apply()
	must(t, os.Remove(f.path("codex-skill/SKILL.md")))
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("orphan policy accepted")
	}
	f = invocationFixture(t)
	f.raw.Resources[0].TranslateSkillInvocation = false
	f.load()
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("policy accepted without opt-in")
	}
}
