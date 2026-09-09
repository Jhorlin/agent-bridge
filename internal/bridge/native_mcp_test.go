package bridge

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// Test binary doubles as a tiny stdio MCP fixture. It never reads files,
// credentials or the network; its only resource is a fixed string.
func TestNativeMCPServerHelper(t *testing.T) {
	if os.Getenv("AGENT_BRIDGE_MCP_FIXTURE") != "1" {
		return
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params map[string]any  `json:"params"`
		}
		if err := decoder.Decode(&request); err != nil {
			if err == io.EOF {
				os.Exit(0)
			}
			os.Exit(2)
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": request.Params["protocolVersion"], "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}}, "serverInfo": map[string]string{"name": "bridge-fixture", "version": "1.0.0"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "bridge_echo", "description": "Return a fixed test string", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "bridge-fixture-ok"}}}
		case "resources/list":
			result = map[string]any{"resources": []any{map[string]string{"uri": "bridge://fixture", "name": "bridge-fixture", "mimeType": "text/plain"}}}
		case "resources/templates/list":
			result = map[string]any{"resourceTemplates": []any{}}
		case "resources/read":
			result = map[string]any{"contents": []any{map[string]string{"uri": "bridge://fixture", "mimeType": "text/plain", "text": "bridge-fixture-ok"}}}
		default:
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "Method not found"}}); err != nil {
				os.Exit(2)
			}
			continue
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			os.Exit(2)
		}
	}
}

func TestNativeMCPStartupAndResourceRead(t *testing.T) {
	f := newFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources = []resourceInput{{ID: "runtime", Kind: "mcp-config", Scope: "project", Claude: ".mcp.json", Codex: "codex-home/config.toml", Servers: []string{"bridge-test"}, AllowReformat: true}}
	f.load()
	binary, err := os.Executable()
	must(t, err)
	data, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"bridge-test": map[string]any{"command": "/usr/bin/env", "args": []string{"AGENT_BRIDGE_MCP_FIXTURE=1", binary, "-test.run=^TestNativeMCPServerHelper$"}}}})
	must(t, err)
	f.write(".mcp.json", string(data))
	f.apply()
	// Approval covers only our reviewed, fixed fixture in the disposable user root.
	f.write("claude-home/settings.json", `{"enabledMcpjsonServers":["bridge-test"]}`)
	output := nativeRun(t, f, tools["claude"], "mcp", "list")
	if !strings.Contains(output, "bridge-test") || !strings.Contains(output, "Connected") {
		t.Fatalf("Claude fixture not connected: %s", output)
	}
	status := nativeRPC(t, f, tools["codex"], "mcpServerStatus/list", map[string]any{"detail": "full"})
	if !strings.Contains(string(status), "bridge_echo") {
		t.Fatalf("Codex did not discover fixture tool: %s", status)
	}
	resource := nativeRPC(t, f, tools["codex"], "mcpServer/resource/read", map[string]any{"server": "bridge-test", "uri": "bridge://fixture"})
	if !strings.Contains(string(resource), "bridge-fixture-ok") {
		t.Fatalf("Codex resource not read: %s", resource)
	}
	nativeRPCSession(t, f, tools["codex"], func(call func(string, any) json.RawMessage) {
		// An ephemeral conversation provides a tool context; no turn is started.
		started := call("thread/start", map[string]any{"cwd": f.dir, "ephemeral": true, "approvalPolicy": "never", "sandbox": "read-only"})
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		must(t, json.Unmarshal(started, &response))
		if response.Thread.ID == "" {
			t.Fatal("missing fixture thread ID")
		}
		result := call("mcpServer/tool/call", map[string]any{"threadId": response.Thread.ID, "server": "bridge-test", "tool": "bridge_echo", "arguments": map[string]any{}})
		if !strings.Contains(string(result), "bridge-fixture-ok") {
			t.Fatalf("fixture tool did not execute: %s", result)
		}
	})
}
