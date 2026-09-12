package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// This characterization bypasses bridge adoption deliberately. Plugin-command
// compatibility imports and standalone SKILL.md loading use different native
// paths; neither is a substitute for checking the other's argument contract.
func TestNativeStandaloneSkillArguments(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			const body = "BRIDGE_SKILL_ARGUMENT_START $ARGUMENTS BRIDGE_SKILL_ARGUMENT_END\n"
			const args = `alpha "two words" $1`
			const expanded = `BRIDGE_SKILL_ARGUMENT_START alpha "two words" $1 BRIDGE_SKILL_ARGUMENT_END`
			const entry = "---\nname: bridge-arguments\ndescription: Inert standalone argument fixture.\n---\n" + body
			f.write("claude-home/skills/bridge-arguments/SKILL.md", entry)
			f.write("home/.agents/skills/bridge-arguments/SKILL.md", entry)
			server, requests := nativePluginFixtureProvider(t)
			if host == "codex" {
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
				nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "$bridge-arguments "+args)
			} else {
				nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "/bridge-arguments "+args)
			}
			want, forbidden := expanded, strings.TrimSpace(body)
			if host == "codex" {
				// Codex loads the body literally. Replacing this expectation with
				// expanded fails against 0.153.4, unlike Claude Code 2.1.268.
				want, forbidden = body, expanded
			}
			// Retain the captured requests for the positive helper while checking
			// that a host has not started sending both competing bodies.
			for n := len(requests); n > 0; n-- {
				raw := <-requests
				var value any
				must(t, json.Unmarshal([]byte(raw), &value))
				if skillArgumentTextContains(value, forbidden) {
					t.Fatal("native request included the opposite argument-loading body")
				}
				requests <- raw
			}
			assertNativeCommandExpansionBody(t, requests, want)
		})
	}
}

func skillArgumentTextContains(value any, text string) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, text)
	case []any:
		for _, child := range v {
			if skillArgumentTextContains(child, text) {
				return true
			}
		}
	case map[string]any:
		for _, child := range v {
			if skillArgumentTextContains(child, text) {
				return true
			}
		}
	}
	return false
}

// Literal catalog prices and GraphQL non-null notation triggered conservative
// automatic-adoption guards during monorepo onboarding. Check actual native
// loading independently of the explicit reviewed-copy fixture.
func TestNativeStandaloneSkillLiteralCatalogExamples(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			const body = "BRIDGE_CATALOG_START price $2.50/M, range $2-3, GraphQL `Float!` BRIDGE_CATALOG_END\n"
			const entry = "---\nname: bridge-catalog\ndescription: Inert catalog notation fixture.\n---\n" + body
			f.write("claude-home/skills/bridge-catalog/SKILL.md", entry)
			f.write("home/.agents/skills/bridge-catalog/SKILL.md", entry)
			server, requests := nativePluginFixtureProvider(t)
			if host == "codex" {
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
				nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "$bridge-catalog alpha beta gamma")
			} else {
				nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "/bridge-catalog alpha beta gamma")
			}
			want := strings.TrimSpace(body)
			if host == "claude" {
				// Claude 2.1.269 interprets $2 as the third argument even inside
				// prices. Codex 0.153.4 preserves the literal source instead.
				// Keep the conservative guard; a copied catalog is not proof of
				// identical native invocation semantics.
				want = "BRIDGE_CATALOG_START price gamma.50/M, range gamma-3, GraphQL `Float!` BRIDGE_CATALOG_END"
			}
			assertNativeCommandExpansionBody(t, requests, want)
		})
	}
}
