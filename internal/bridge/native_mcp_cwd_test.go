package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Characterize the native mismatch that makes cwd unsafe in the common model.
// The script only records its working directory and runs the inert MCP helper.
func TestNativeClaudeIgnoresMCPCWD(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	binary, err := os.Executable()
	must(t, err)
	must(t, os.MkdirAll(f.path("requested working directory"), 0700))
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	f.write("capture-cwd.sh", "#!/bin/sh\npwd -P > "+quote(f.path("actual-cwd"))+"\nexec /usr/bin/env AGENT_BRIDGE_MCP_FIXTURE=1 "+quote(binary)+" '-test.run=^TestNativeMCPServerHelper$'\n")
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"bridge-cwd": map[string]any{
		"command": "/bin/sh", "args": []string{f.path("capture-cwd.sh")}, "cwd": f.path("requested working directory"),
	}}})
	must(t, err)
	// This unsupported native input is deliberately not passed through Apply.
	f.write(".mcp.json", string(data))
	f.write("claude-home/settings.json", `{"enabledMcpjsonServers":["bridge-cwd"]}`)
	output := nativeRun(t, f, tools["claude"], "mcp", "list")
	if !strings.Contains(output, "bridge-cwd") || !strings.Contains(output, "Connected") {
		t.Fatalf("fixture MCP server did not connect: %s", output)
	}
	if got := strings.TrimSpace(f.read("actual-cwd")); got != f.dir {
		t.Fatalf("Claude cwd behavior changed; reassess the compatibility boundary: got %q, want session directory %q", got, f.dir)
	}
	t.Log("Claude started the inert server in the session directory, ignoring its different existing absolute cwd")
}
