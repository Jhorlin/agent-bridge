package bridge

import (
	"strings"
	"testing"
)

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
