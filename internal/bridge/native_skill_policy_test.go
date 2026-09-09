package bridge

import (
	"fmt"
	"strings"
	"testing"
)

// Establish the native policy contract before adding a one-to-many adapter.
func TestNativeSkillInvocationPolicy(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		for _, disabled := range []bool{false, true} {
			for _, explicit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/disabled=%v/explicit=%v", host, disabled, explicit), func(t *testing.T) {
					f := newFixture(t)
					tools := nativeTools(t, f)
					const description = "BRIDGE_INVOCATION_POLICY_DESCRIPTION"
					const body = "BRIDGE_INVOCATION_POLICY_BODY"
					f.write("claude-home/skills/bridge-policy/SKILL.md", fmt.Sprintf("---\nname: bridge-policy\ndescription: %s\ndisable-model-invocation: %v\n---\n%s\n", description, disabled, body))
					f.raw.Resources = []resourceInput{{ID: "policy", Kind: "skill-directory", Scope: "global", Portable: true, AllowReformat: true, TranslateSkillInvocation: true, Claude: "claude-home/skills/bridge-policy", Codex: "home/.agents/skills/bridge-policy"}}
					f.load()
					f.apply()
					server, requests := nativePluginFixtureProvider(t)
					prompt := "Return fixture complete."
					if host == "codex" {
						f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
						if explicit {
							prompt = "$bridge-policy"
						}
						nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", prompt)
					} else {
						if explicit {
							prompt = "/bridge-policy"
						}
						nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", prompt)
					}
					if len(requests) == 0 {
						t.Fatal("native host made no fixture request")
					}
					seenDescription, seenBody := false, false
					for len(requests) > 0 {
						request := <-requests
						seenDescription = seenDescription || strings.Contains(request, description)
						seenBody = seenBody || strings.Contains(request, body)
					}
					if explicit {
						if !seenBody {
							t.Fatal("explicit invocation did not load skill body")
						}
					} else if seenDescription == disabled || seenBody {
						t.Fatal("unexpected implicit skill exposure", seenDescription, seenBody)
					}
				})
			}
		}
	}
}
