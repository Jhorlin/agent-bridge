package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// These fixtures bypass bridge validation deliberately: they distinguish native
// literal text from substitutions that Codex drops or leaves unexpanded. No
// copied user plugin or real model is invoked.
func TestNativePluginCommandExpansionSubset(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			type command struct {
				file, codexName, body, claudeBody string
				codexOmitted                      bool
			}
			commands := []command{
				{"literal", "literal", `FIXTURE_LITERAL regex \.env$|node_modules/; $branch $HOME ${HOME} ${UNKNOWN} ${ARGUMENTS}; $ USD; https://example.invalid/$(printf inert)/file`, "", false},
				{"group/nested", "group-nested", "FIXTURE_NESTED", "", false},
				{"other/nested", "other-nested", "FIXTURE_OTHER", "", false},
				{"clean_gone", "clean-gone", "FIXTURE_UNDERSCORE", "", false},
				{"settings", "settings", "FIXTURE_SETTINGS", "", false},
				{"arguments", "arguments", "FIXTURE_ARGUMENTS $ARGUMENTS", "FIXTURE_ARGUMENTS alpha beta", true},
				{"position", "position", "FIXTURE_POSITION $0 $1.00", "FIXTURE_POSITION alpha beta.00", true},
				{"escaped", "escaped", `FIXTURE_ESCAPED \$1 \$ARGUMENTS`, "FIXTURE_ESCAPED $1 $ARGUMENTS", true},
				{"root", "root", "FIXTURE_ROOT ${CLAUDE_PLUGIN_ROOT}", "FIXTURE_ROOT " + f.path("market/package"), false},
				{"unbraced", "unbraced", "FIXTURE_UNBRACED $CLAUDE_PLUGIN_ROOT $CLAUDE_SESSION_ID", "", false},
			}
			for _, c := range commands {
				frontmatter := "---\ndescription: Inert " + c.codexName + " command.\n"
				if c.file == "settings" {
					frontmatter += "argument-hint: [project-name]\ndisable-model-invocation: false\n"
				}
				f.write("market/package/commands/"+c.file+".md", frontmatter+"---\n"+c.body+"\n")
			}
			nativeCommandExpansionInstall(t, f, tools[host], host)
			server, requests := nativePluginFixtureProvider(t)
			var skills map[string]bool
			if host == "codex" {
				f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n"+f.read("codex-home/config.toml"))
				skills = nativeCommandExpansionSkills(t, f, tools[host])
			}
			for _, c := range commands {
				t.Run(c.file, func(t *testing.T) {
					want := c.body
					if host == "codex" {
						if skills["demo:source-command-"+c.codexName] == c.codexOmitted {
							t.Fatal("native command discovery disagrees with expected substitution support")
						}
						if c.codexOmitted {
							return
						}
						nativeRun(t, f, tools[host], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "$demo:source-command-"+c.codexName+" alpha beta")
					} else {
						if c.claudeBody != "" {
							want = c.claudeBody
						}
						name := strings.ReplaceAll(c.file, "/", ":")
						nativeRunEnvironment(t, f, tools[host], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "/demo:"+name+" alpha beta")
					}
					assertNativeCommandExpansionBody(t, requests, want)
				})
			}
		})
	}
}

// Codex's compatibility importer silently omits both commands when their paths
// collapse to the same migrated skill name; innocent siblings still load.
func TestNativePluginCommandExpansionNameCollisions(t *testing.T) {
	for _, names := range [][]string{{"group/nested", "group-nested"}, {"clean_gone", "clean-gone"}, {"one__two", "one--two"}} {
		t.Run(names[0], func(t *testing.T) {
			f := newFixture(t)
			tools := nativeTools(t, f)
			for _, name := range append(names, "control") {
				f.write("market/package/commands/"+name+".md", "---\ndescription: Inert collision fixture.\n---\nReturn fixture complete.\n")
			}
			nativeCommandExpansionInstall(t, f, tools["codex"], "codex")
			if got := nativeCommandExpansionSkills(t, f, tools["codex"]); len(got) != 1 || !got["demo:source-command-control"] {
				t.Fatalf("native collision behavior changed: %v", got)
			}
		})
	}
}

func nativeCommandExpansionInstall(t *testing.T, f *fixture, binary, host string) {
	t.Helper()
	if host == "claude" {
		f.write("market/package/.claude-plugin/plugin.json", `{"name":"demo","version":"1.0.0","description":"Inert command fixture"}`)
		f.write("market/.claude-plugin/marketplace.json", `{"name":"command-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./package"}]}`)
		nativeRun(t, f, binary, "plugin", "marketplace", "add", f.path("market"))
		nativeRun(t, f, binary, "plugin", "install", "demo@command-fixture", "--scope", "user")
		return
	}
	f.write("market/package/.codex-plugin/plugin.json", `{"name":"demo","version":"1.0.0","description":"Inert command fixture"}`)
	f.write("market/.agents/plugins/marketplace.json", `{"name":"command-fixture","interface":{"displayName":"Bridge tests"},"plugins":[{"name":"demo","source":{"source":"local","path":"./package"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	nativeRun(t, f, binary, "plugin", "marketplace", "add", f.path("market"), "--json")
	nativeRun(t, f, binary, "plugin", "add", "demo@command-fixture", "--json")
}

func nativeCommandExpansionSkills(t *testing.T, f *fixture, binary string) map[string]bool {
	t.Helper()
	raw := nativeRPC(t, f, binary, "skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
	var doc struct {
		Data []struct {
			Skills []struct{ Name, PluginID string }
			Errors []any
		}
	}
	must(t, json.Unmarshal(raw, &doc))
	result := map[string]bool{}
	for _, data := range doc.Data {
		if len(data.Errors) != 0 {
			t.Fatal("unexpected native skill discovery errors")
		}
		for _, skill := range data.Skills {
			if skill.PluginID == "demo@command-fixture" {
				result[skill.Name] = true
			}
		}
	}
	return result
}

func assertNativeCommandExpansionBody(t *testing.T, requests chan string, want string) {
	t.Helper()
	found := false
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case string:
			found = found || strings.Contains(v, want)
		case []any:
			for _, item := range v {
				visit(item)
			}
		case map[string]any:
			for _, item := range v {
				visit(item)
			}
		}
	}
	for len(requests) > 0 {
		var value any
		must(t, json.Unmarshal([]byte(<-requests), &value))
		visit(value)
	}
	if !found {
		t.Fatal("native request did not preserve the expected inert command body")
	}
}
