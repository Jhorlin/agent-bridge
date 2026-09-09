package bridge

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
)

func strictSkillFixture(t *testing.T) *fixture {
	f := skillFixture(t)
	f.raw.Resources[0].AllowReformat = true
	f.load()
	return f
}

func TestStrictSkillRoundTripAndIndependentEdits(t *testing.T) {
	f := strictSkillFixture(t)
	f.apply()
	if f.plan().HasConflicts() {
		t.Fatal("initial sync conflict")
	}
	before := f.read("claude-skill/SKILL.md")
	f.write("codex-skill/SKILL.md", "---\ndescription: 'A portable demo.' # formatting only\nname: demo\n---\nUse scripts/run.sh.\n")
	for _, item := range f.plan().Items {
		if len(item.Writes) != 0 {
			t.Fatal("formatting caused drift")
		}
	}
	f.apply()
	f.expect("claude-skill/SKILL.md", before)
	f.write("codex-skill/SKILL.md", "---\nname: demo\ndescription: Updated description.\n---\nNew instruction body.\r\n")
	f.write("claude-skill/references/context.txt", "independent reference")
	f.apply()
	for _, side := range []string{"claude-skill", "codex-skill", "state/shared/demo"} {
		if !strings.Contains(f.read(side+"/SKILL.md"), "Updated description.") || !strings.HasSuffix(f.read(side+"/SKILL.md"), "New instruction body.\r\n") {
			t.Fatal("metadata or body lost")
		}
		f.expect(side+"/references/context.txt", "independent reference")
		info, err := os.Stat(f.path(side + "/scripts/run.sh"))
		must(t, err)
		if info.Mode().Perm()&0111 == 0 {
			t.Fatal("lost script executable bits")
		}
	}
	for _, item := range f.plan().Items {
		if len(item.Writes) != 0 {
			t.Fatal("not idempotent")
		}
	}
}

func TestStrictSkillRejectsUnsupportedAndMalformed(t *testing.T) {
	for _, front := range []string{
		"!custom {name: demo, description: ok}",
		"&metadata {name: demo, description: ok}",
		"name: demo\ndescription: ok\nmodel: secret-value",
		"name: demo\ndescription: ok\ndisable-model-invocation: true",
		"name: demo\ndescription: ok\nallowed-tools: Bash",
		"name: demo\ndescription: ok\nname: other",
		"name: demo\ndescription: !!binary b2s=",
		"name: demo\ndescription: &d ok",
		"name: demo\ndescription: [bad]",
		"name: demo\ndescription: 42",
		"name: Upper\ndescription: ok",
		"name: bad--name\ndescription: ok",
		"name: demo\ndescription: \"line\\nline\"",
		"name: demo",
		"name: demo\ndescription: ok\nunknown-secret-key: private-value",
	} {
		t.Run(front, func(t *testing.T) {
			f := strictSkillFixture(t)
			f.write("claude-skill/SKILL.md", "---\n"+front+"\n---\nInstruction.")
			_, err := Apply(f.c, Options{})
			if err == nil {
				t.Fatal("accepted unsupported metadata")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private-value") {
				t.Fatal("leaked metadata")
			}
			f.missing("codex-skill/SKILL.md")
			f.missing("state/pending.json")
			if !Audit(f.c).Blocked() {
				t.Fatal("audit accepted rejected metadata")
			}
		})
	}
	for _, input := range []string{"no metadata", "---\nname: demo\n", "---\nname: demo\ndescription: ok\n---\n", "---\nname: demo\ndescription: ok\n---\n\xff"} {
		if _, err := normalizeSkill(&Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}); err == nil {
			t.Fatal("accepted malformed skill")
		}
	}
}

func TestStrictSkillRejectsSidecarAndPreservesRawMode(t *testing.T) {
	f := strictSkillFixture(t)
	f.write("claude-skill/agents/openai.yaml", "policy:\n  allow_implicit_invocation: false\n")
	if _, err := Plan(f.c); err == nil {
		t.Fatal("silently copied host policy")
	}
	f.raw.Resources[0].AllowReformat = false
	f.load()
	f.apply()
	f.expect("codex-skill/agents/openai.yaml", f.read("claude-skill/agents/openai.yaml"))
	f.raw.Resources[0].AllowReformat = true
	f.load()
	if _, err := Plan(f.c); err == nil {
		t.Fatal("silently changed resource identity")
	}
}

func TestStrictSkillConflictDeletionAndRollback(t *testing.T) {
	f := strictSkillFixture(t)
	f.apply()
	original := f.read("claude-skill/SKILL.md")
	changed := strings.Replace(original, "A portable demo.", "Changed description.", 1)
	f.write("claude-skill/SKILL.md", changed)
	f.write("codex-skill/SKILL.md", strings.Replace(original, "A portable demo.", "Different description.", 1))
	if !f.plan().HasConflicts() {
		t.Fatal("lost conflict")
	}
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("wrote conflicting skill")
	}
	f.write("codex-skill/SKILL.md", original)
	_, err := Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
		if index == 1 {
			return errors.New("injected failure")
		}
		return nil
	}})
	if err == nil {
		t.Fatal("failure not injected")
	}
	f.expect("claude-skill/SKILL.md", changed)
	f.expect("codex-skill/SKILL.md", original)
	f.expect("state/shared/demo/SKILL.md", original)
	f.apply()
	must(t, os.Remove(f.path("codex-skill/SKILL.md")))
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("propagated deletion")
	}
}

func FuzzStrictSkillRoundTrip(f *testing.F) {
	f.Add("---\nname: demo\ndescription: Demo.\n---\nInstruction.\r\n")
	f.Add("---\nname: demo\ndescription: !!binary /Q==\n---\nInstruction.")
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			return
		}
		raw := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0700}
		common, err := normalizeSkill(raw)
		if err != nil {
			return
		}
		again, err := normalizeSkill(common)
		if err != nil || !equal(common, again) {
			t.Fatal("accepted skill did not round-trip")
		}
	})
}
