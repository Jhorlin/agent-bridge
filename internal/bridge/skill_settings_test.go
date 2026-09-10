package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillNativeSettingsPreservedNotGranted(t *testing.T) {
	f := invocationFixture(t)
	f.raw.Resources[0].PreserveSkillSettings = true
	f.load()
	f.write("claude-skill/SKILL.md", strings.Replace(f.read("claude-skill/SKILL.md"), "description:", "allowed-tools: 'Bash(example:*) Read'\nargument-hint: '[target]'\nversion: 1.0.0\nmcp: [fixture-browser]\ndescription:", 1))
	before := f.read("claude-skill/SKILL.md")
	f.apply()
	f.expect("claude-skill/SKILL.md", before)
	for _, key := range []string{"allowed-tools", "argument-hint", "version:", "mcp:"} {
		if strings.Contains(f.read("codex-skill/SKILL.md"), key) || strings.Contains(f.read("state/shared/demo/SKILL.md"), key) {
			t.Fatal("native metadata or grant copied")
		}
	}
	f.write("codex-skill/SKILL.md", strings.Replace(f.read("codex-skill/SKILL.md"), "Exact body.", "Revised body.", 1))
	f.apply()
	claude := f.read("claude-skill/SKILL.md")
	for _, marker := range []string{"allowed-tools:", "Bash(example:*) Read", "argument-hint:", "version:", "fixture-browser", "Revised body."} {
		if !strings.Contains(claude, marker) {
			t.Fatal("native field or reverse edit lost", marker)
		}
	}
	f.apply()
}

func TestSkillSettingsOptInAndUnsafeShapes(t *testing.T) {
	for _, field := range []string{"allowed-tools: 12", "argument-hint: [bad]", "version: true", "mcp: {server: bad}", "context: fork", "disallowed-tools: Bash", "hooks: {}"} {
		f := invocationFixture(t)
		f.raw.Resources[0].PreserveSkillSettings = true
		f.load()
		f.write("claude-skill/SKILL.md", strings.Replace(f.read("claude-skill/SKILL.md"), "description:", field+"\ndescription:", 1))
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("invalid or behavioral field accepted", field)
		}
		f.missing("codex-skill/SKILL.md")
	}
	f := invocationFixture(t)
	f.write("claude-skill/SKILL.md", strings.Replace(f.read("claude-skill/SKILL.md"), "description:", "allowed-tools: Read\ndescription:", 1))
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("native metadata accepted without opt-in")
	}
}

func TestConventionSkillSettingsUpgradeAndReload(t *testing.T) {
	f := allConventionFixture(t, "project")
	f.write(".claude/skills/demo/SKILL.md", "---\nname: demo\ndescription: Demo.\n---\nBody.\r\n")
	reloadConventions(t, f)
	f.apply()
	profile := strings.Replace(f.read("config.json"), `"conventions":{`, `"conventions":{"preserveSkillSettings":true,`, 1)
	f.write("config.json", profile)
	reloadConventions(t, f)
	f.write(".claude/skills/demo/SKILL.md", strings.Replace(f.read(".claude/skills/demo/SKILL.md"), "description:", "allowed-tools: [Read, Grep]\ndescription:", 1))
	f.apply()
	if !strings.HasSuffix(f.read(".agents/skills/demo/SKILL.md"), "Body.\r\n") {
		t.Fatal("body line endings changed")
	}
	reloadConventions(t, f)
	f.apply()
	flat, err := flatProfile(f.c)
	must(t, err)
	var raw configInput
	must(t, decode(flat, &raw))
	if !raw.Conventions.PreserveSkillSettings {
		t.Fatal("convention option lost")
	}
	for _, r := range f.c.Resources {
		if r.Kind == "skill-directory" {
			data, err := json.Marshal(r)
			must(t, err)
			var copy Resource
			must(t, json.Unmarshal(data, &copy))
			if !copy.PreserveSkillSettings {
				t.Fatal("manifest option lost")
			}
		}
	}
	f.write("config.json", strings.Replace(profile, `"preserveSkillSettings":true,`, "", 1))
	if _, err := LoadConfig(f.path("config.json")); err == nil {
		t.Fatal("policy downgrade accepted")
	}
}

func TestSkillNativeSettingsRawObservation(t *testing.T) {
	f := invocationFixture(t)
	f.raw.Resources[0].PreserveSkillSettings = true
	f.load()
	f.write("claude-skill/SKILL.md", strings.Replace(f.read("claude-skill/SKILL.md"), "description:", "allowed-tools: Read\ndescription:", 1))
	f.apply()
	plan, err := Plan(f.c)
	must(t, err)
	f.write("claude-skill/SKILL.md", strings.Replace(f.read("claude-skill/SKILL.md"), "allowed-tools: Read", "allowed-tools: Grep", 1))
	if _, err := Apply(f.c, Options{ExpectedObservation: Observation(f.c, plan)}); err == nil {
		t.Fatal("raw setting edit did not stale observation")
	}
	f.apply()
	if !strings.Contains(f.read("claude-skill/SKILL.md"), "allowed-tools: Grep") {
		t.Fatal("local setting lost")
	}
}
