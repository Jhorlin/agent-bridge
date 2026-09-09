package bridge

import (
	"strings"
	"testing"
)

func TestNativePluginAgentExport(t *testing.T) {
	f := pluginAgentFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].PreserveAgentSettings = true
	f.load()
	f.write("claude-plugin/agents/reviewer.md", strings.Replace(f.read("claude-plugin/agents/reviewer.md"), "name: reviewer\n", "name: reviewer\nmodel: inherit\n", 1))
	f.apply()
	server, requests := nativePluginFixtureProvider(t)
	f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n[features]\nplugins=false\n")
	nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
	assertNativeRequestMarker(t, requests, "BRIDGE_PLUGIN_EXPORT_DESCRIPTION")
	f.write("codex-home/agents/bridge-bundle-reviewer.toml", strings.ReplaceAll(f.read("codex-home/agents/bridge-bundle-reviewer.toml"), "BRIDGE_PLUGIN_EXPORT_DESCRIPTION", "BRIDGE_PLUGIN_REVERSE_DESCRIPTION")+"sandbox_mode='read-only'\nmodel_reasoning_effort='low'\n")
	f.apply()
	if !strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "model: inherit") {
		t.Fatal("reverse export lost Claude model choice")
	}
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "add", f.dir)
	nativeRun(t, f, tools["claude"], "plugin", "install", "demo@bridge-fixture", "--scope", "user")
	nativeRun(t, f, tools["claude"], "plugin", "validate", f.path("claude-plugin"))
	nativeRunEnvironment(t, f, tools["claude"], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "Return fixture complete.")
	assertNativeRequestMarker(t, requests, "BRIDGE_PLUGIN_REVERSE_DESCRIPTION")
	nativeRun(t, f, tools["claude"], "plugin", "uninstall", "demo@bridge-fixture", "--scope", "user")
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "remove", "bridge-fixture")
	f.missing("codex-plugin/agents/reviewer.md")
}

func TestNativePluginAgentCompatibilityBoundary(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	f.load()
	f.apply()
	// Direct inert probe, not bridge-supported output yet.
	f.write("home/plugins/demo/agents/bridge-reviewer.md", "---\nname: bridge-reviewer\ndescription: BRIDGE_PLUGIN_AGENT_DESCRIPTION\n---\nBRIDGE_PLUGIN_AGENT_BODY\n")
	f.write("home/plugins/demo/agents/bridge-toml.toml", "name='bridge-toml'\ndescription='BRIDGE_PLUGIN_TOML_DESCRIPTION'\ndeveloper_instructions='BRIDGE_PLUGIN_TOML_BODY'\n")
	f.write("codex-home/agents/control.toml", "name='bridge-control'\ndescription='BRIDGE_CONTROL_AGENT_DESCRIPTION'\ndeveloper_instructions='BRIDGE_CONTROL_AGENT_BODY'\n")
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	server, requests := nativePluginFixtureProvider(t)
	f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL))
	nativeRPC(t, f, tools["codex"], "plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
	nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
	found, control := false, false
	for len(requests) > 0 {
		body := <-requests
		found = strings.Contains(body, "BRIDGE_PLUGIN_AGENT_DESCRIPTION") || strings.Contains(body, "BRIDGE_PLUGIN_TOML_DESCRIPTION") || found
		control = strings.Contains(body, "BRIDGE_CONTROL_AGENT_DESCRIPTION") || control
	}
	if !control {
		t.Fatal("positive-control standalone agent was not advertised")
	}
	if found {
		t.Fatal("native bundled-agent discovery changed; reassess the bridge compatibility boundary")
	}
	nativeRPC(t, f, tools["codex"], "plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
}
