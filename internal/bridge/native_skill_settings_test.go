package bridge

import (
	"strings"
	"testing"
)

func TestNativeSkillLocalSettings(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			f.raw.Resources = []resourceInput{{ID: "settings", Kind: "skill-directory", Scope: "global", Portable: true, AllowReformat: true, TranslateSkillInvocation: true, PreserveSkillSettings: true, Claude: "claude-home/skills/bridge-settings", Codex: "home/.agents/skills/bridge-settings"}}
			f.load()
			f.write("claude-home/skills/bridge-settings/SKILL.md", "---\nname: bridge-settings\ndescription: Fixture settings skill.\nallowed-tools: Read\nargument-hint: '[fixture]'\nversion: 1.0.0\nmcp: [fixture-only]\n---\nBRIDGE_NATIVE_LOCAL_SETTINGS_BODY\n")
			f.apply()
			server, requests := nativePluginFixtureProvider(t)
			if host == "codex" {
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
				nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "$bridge-settings")
			} else {
				nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "/bridge-settings")
			}
			seen := false
			for len(requests) > 0 {
				seen = strings.Contains(<-requests, "BRIDGE_NATIVE_LOCAL_SETTINGS_BODY") || seen
			}
			if !seen {
				t.Fatal("native host did not load preserved-settings skill")
			}
			if strings.Contains(f.read("home/.agents/skills/bridge-settings/SKILL.md"), "allowed-tools") {
				t.Fatal("grant leaked to Codex")
			}
		})
	}
}
