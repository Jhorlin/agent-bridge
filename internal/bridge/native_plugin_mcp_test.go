package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Probe conventional bundled MCP loading separately from conversion. Only this
// test binary's inert stdio helper is installed into disposable host homes.
func TestNativeBundledMCPContract(t *testing.T) {
	nativeBundledMCP(t, "")
}

func TestNativeBundledMCPRootCompatibilityBoundary(t *testing.T) {
	nativeBundledMCP(t, "${CLAUDE_PLUGIN_ROOT}/scripts/mcp-fixture")
}

func nativeBundledMCP(t *testing.T, codexRootCommand string) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	f.raw.Resources[0].Servers = []string{"bridge-test"}
	f.raw.Resources[0].AllowReformat = true
	f.load()
	binary, err := os.Executable()
	must(t, err)
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"bridge-test": map[string]any{"command": "/usr/bin/env", "args": []string{"AGENT_BRIDGE_MCP_FIXTURE=1", binary, "-test.run=^TestNativeMCPServerHelper$"}}}})
	must(t, err)
	f.write("claude-plugin/.mcp.json", string(data))
	f.apply()
	if codexRootCommand != "" {
		// Native-contract probe: package-root substitutions are not yet accepted
		// by the adapter, so install only this reviewed generated fixture directly.
		data, err = json.Marshal(map[string]any{"mcpServers": map[string]any{"bridge-test": map[string]any{"command": "${CLAUDE_PLUGIN_ROOT}/scripts/mcp-fixture"}}})
		must(t, err)
		for _, root := range []string{"claude-plugin", "home/plugins/demo"} {
			if root == "home/plugins/demo" {
				server := map[string]any{"command": codexRootCommand}
				data, err = json.Marshal(map[string]any{"mcpServers": map[string]any{"bridge-test": server}})
				must(t, err)
			}
			f.write(root+"/scripts/mcp-fixture", "#!/bin/sh\nexec /usr/bin/env AGENT_BRIDGE_MCP_FIXTURE=1 '"+binary+"' '-test.run=^TestNativeMCPServerHelper$'\n")
			must(t, os.Chmod(f.path(root+"/scripts/mcp-fixture"), 0755))
			f.write(root+"/.mcp.json", string(data))
		}
	}
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "add", f.dir)
	nativeRun(t, f, tools["claude"], "plugin", "install", "demo@bridge-fixture", "--scope", "user")
	output := nativeRun(t, f, tools["claude"], "mcp", "list")
	if !strings.Contains(output, "bridge-test") || !strings.Contains(output, "Connected") {
		t.Fatalf("Claude bundled MCP not connected: %s", output)
	}
	nativeRun(t, f, tools["claude"], "plugin", "uninstall", "demo@bridge-fixture", "--scope", "user")
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "remove", "bridge-fixture")
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	nativeRPCSession(t, f, tools["codex"], func(call func(string, any) json.RawMessage) {
		call("plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
	})
	status := nativeRPC(t, f, tools["codex"], "mcpServerStatus/list", map[string]any{"detail": "full"})
	if codexRootCommand != "" {
		if strings.Contains(string(status), "bridge_echo") || !strings.Contains(string(status), `"pluginId":"demo@personal"`) {
			t.Fatalf("bundled root-path behavior changed; reassess the compatibility boundary: %s", status)
		}
		t.Log("Claude connected its root-relative fixture; Codex recognized the plugin server but did not discover its tool. Root-path conversion remains blocked.")
	} else if !strings.Contains(string(status), "bridge_echo") {
		t.Fatalf("Codex bundled MCP not discovered: %s", status)
	}
	nativeRPCSession(t, f, tools["codex"], func(call func(string, any) json.RawMessage) {
		call("plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
	})
}
