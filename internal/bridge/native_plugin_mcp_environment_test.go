package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// A launcher cannot assume hooks' root environment also exists for bundled MCP.
// Use an absolute inert script argument, with no manifest root substitutions,
// to distinguish that environment from successful command-path expansion.
func TestNativePluginMCPRootEnvironmentAndWorkingDirectory(t *testing.T) {
	for _, spec := range []struct{ host, layout string }{{"claude", "compatibility"}, {"codex", "compatibility"}, {"codex", "portable"}} {
		t.Run(spec.host+"/"+spec.layout, func(t *testing.T) {
			f := newFixture(t)
			f.dir = f.path("fixture with spaces")
			must(t, os.MkdirAll(f.dir, 0700))
			tools := nativeTools(t, f)
			binary, err := os.Executable()
			must(t, err)
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
			// Never capture the complete environment: these two root keys and cwd
			// are the entire observable contract needed by the bridge.
			f.write("market/package/scripts/control.sh", "#!/bin/sh\n"+
				"printf 'CLAUDE_PLUGIN_ROOT=%s\\nPLUGIN_ROOT=%s\\ncwd=%s\\n' \"${CLAUDE_PLUGIN_ROOT-unset}\" \"${PLUGIN_ROOT-unset}\" \"$(pwd -P)\" > "+quote(f.path("capture.txt"))+"\n"+
				"exec /usr/bin/env AGENT_BRIDGE_MCP_FIXTURE=1 "+quote(binary)+" '-test.run=^TestNativeMCPServerHelper$'\n")
			// Bare sh resolves only through nativeEnvironment's system-only PATH.
			// The absolute script is deliberately independent of the native cwd.
			server := map[string]any{"command": "sh", "args": []string{f.path("market/package/scripts/control.sh")}}
			manifest := map[string]any{"name": "demo", "version": "1.0.0", "description": "Inert MCP environment fixture"}
			manifestPath, mcpPath := ".codex-plugin/plugin.json", ".mcp.json"
			mcp := map[string]any{"mcpServers": map[string]any{"environment-fixture": server}}
			if spec.host == "claude" {
				manifestPath = ".claude-plugin/plugin.json"
			} else if spec.layout == "portable" {
				manifestPath, mcpPath = "plugin.json", "mcp.json"
				manifest["$schema"] = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
				mcp["$schema"] = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
				server["type"] = "stdio"
			}
			data, err := json.Marshal(manifest)
			must(t, err)
			f.write("market/package/"+manifestPath, string(data))
			data, err = json.Marshal(mcp)
			must(t, err)
			f.write("market/package/"+mcpPath, string(data))
			wantClaudeRoot, wantPluginRoot, wantCWD := "unset", "unset", f.dir
			if spec.host == "claude" {
				f.write("market/.claude-plugin/marketplace.json", `{"name":"environment-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./package"}]}`)
				nativeRun(t, f, tools[spec.host], "plugin", "marketplace", "add", f.path("market"))
				nativeRun(t, f, tools[spec.host], "plugin", "install", "demo@environment-fixture", "--scope", "user")
				output := nativeRun(t, f, tools[spec.host], "mcp", "list")
				if !strings.Contains(output, "environment-fixture") || !strings.Contains(output, "Connected") {
					t.Fatal("inert Claude MCP did not connect; reassess the compatibility boundary")
				}
				// Local-marketplace Claude currently exposes the authoring package,
				// even though its plugin inventory reports an installed cache path.
				wantClaudeRoot = f.path("market/package")
			} else {
				f.write("market/.agents/plugins/marketplace.json", `{"name":"environment-fixture","interface":{"displayName":"Bridge tests"},"plugins":[{"name":"demo","source":{"source":"local","path":"./package"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
				nativeRun(t, f, tools[spec.host], "plugin", "marketplace", "add", f.path("market"), "--json")
				installed := nativeRun(t, f, tools[spec.host], "plugin", "add", "demo@environment-fixture", "--json")
				var result struct{ InstalledPath string }
				must(t, json.Unmarshal([]byte(installed), &result))
				if result.InstalledPath == "" {
					t.Fatal("native installer returned no fixture cache path")
				}
				status := nativeRPC(t, f, tools[spec.host], "mcpServerStatus/list", map[string]any{"detail": "full"})
				if !strings.Contains(string(status), "bridge_echo") || !strings.Contains(string(status), `"pluginId":"demo@environment-fixture"`) {
					t.Fatal("inert Codex MCP did not connect; reassess the compatibility boundary")
				}
				if spec.layout == "portable" {
					wantPluginRoot, wantCWD = result.InstalledPath, result.InstalledPath
				}
			}
			want := fmt.Sprintf("CLAUDE_PLUGIN_ROOT=%s\nPLUGIN_ROOT=%s\ncwd=%s\n", wantClaudeRoot, wantPluginRoot, wantCWD)
			if got := f.read("capture.txt"); got != want {
				t.Fatalf("native MCP root environment or cwd changed; reassess compatibility: got %q, want %q", got, want)
			}
		})
	}
}
