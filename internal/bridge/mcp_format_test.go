package bridge

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestMCPFormattingTransaction(t *testing.T) {
	f := mcpFixture(t)
	f.write("claude.json", "{\n  \"mcpServers\" : {\"docs\": {\"command\" : \"demo-server\"}}\n}\n")
	f.write("codex.toml", "# private layout\n[mcp_servers.docs]\ncommand   = 'demo-server' # command comment\n")
	f.apply()
	claude, codex := f.read("claude.json"), f.read("codex.toml")
	f.write("claude.json", strings.ReplaceAll(claude, "demo-server", "changed"))
	_, err := Apply(f.c, Options{BeforeWrite: func(index int, _ Operation) error {
		if index == 1 {
			return errors.New("injected formatting rollback")
		}
		return nil
	}})
	if err == nil {
		t.Fatal("expected injected failure")
	}
	f.expect("codex.toml", codex)
	f.apply()
	f.expect("codex.toml", strings.ReplaceAll(codex, "demo-server", "changed"))
	f.write("codex.toml", strings.ReplaceAll(f.read("codex.toml"), "changed", "reverse"))
	f.apply()
	f.expect("claude.json", strings.ReplaceAll(claude, "demo-server", "reverse"))
	before := f.read("codex.toml")
	f.apply()
	f.expect("codex.toml", before)
}

func TestMCPScalarFormatting(t *testing.T) {
	for _, tc := range []struct{ side, input, want string }{
		{"claude", "{\r\n  \"other\" : [1, {\"command\": \"old\"}],\r\n  \"mcpServers\" : {\"demo\": {\"command\" : \"old\"}}\r\n}\r\n", "{\r\n  \"other\" : [1, {\"command\": \"old\"}],\r\n  \"mcpServers\" : {\"demo\": {\"command\" : \"new\"}}\r\n}\r\n"},
		{"codex", "# top\r\n[mcp_servers.'demo'] # table\r\ncommand   = 'old' # retained\r\n\r\n[other]\r\ncommand = 'old'\r\n", "# top\r\n[mcp_servers.'demo'] # table\r\ncommand   = 'new' # retained\r\n\r\n[other]\r\ncommand = 'old'\r\n"},
		{"codex", "mcp_servers.demo.command = \"old\" # note\n", "mcp_servers.demo.command = 'new' # note\n"},
		{"codex", "[mcp_servers.demo]\ncommand = '''old''' # multiline literal\n", "[mcp_servers.demo]\ncommand = 'new' # multiline literal\n"},
	} {
		t.Run(tc.side+tc.input, func(t *testing.T) {
			before := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(tc.input)), Mode: 0640}
			doc, err := document(tc.side, before)
			must(t, err)
			key := "mcpServers"
			if tc.side == "codex" {
				key = "mcp_servers"
			}
			doc[key].(map[string]any)["demo"].(map[string]any)["command"] = "new"
			got := preserveMCPText(tc.side, before, doc)
			if got == nil {
				t.Fatal("scalar patch unexpectedly fell back")
			}
			b, err := base64.StdEncoding.DecodeString(got.Data)
			must(t, err)
			if string(b) != tc.want || got.Mode != before.Mode {
				t.Fatalf("got %q mode %o, want %q", b, got.Mode, tc.want)
			}
		})
	}
}

func TestMCPFormattingStructuralFallback(t *testing.T) {
	for _, input := range []string{
		"[mcp_servers.demo]\ncommand = 'old'\nargs = [\n# keep until explicit reformat\n'one']\n",
		"mcp_servers = { demo = { command = 'old' } }\n",
	} {
		before := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		doc, err := document("codex", before)
		must(t, err)
		server := doc["mcp_servers"].(map[string]any)["demo"].(map[string]any)
		server["command"] = "new"
		if strings.Contains(input, "args") {
			server["args"] = []any{"two"}
		}
		if preserveMCPText("codex", before, doc) != nil {
			t.Fatal("unsupported structural patch must use consented renderer")
		}
	}
}

func FuzzMCPTextPreservation(f *testing.F) {
	f.Add("[mcp_servers.demo]\ncommand = 'old' # retained\n", true)
	f.Add(`{"mcpServers":{"demo":{"command":"old"}}}`, false)
	f.Fuzz(func(t *testing.T, input string, codex bool) {
		if len(input) > 32768 {
			t.Skip()
		}
		side, key := "claude", "mcpServers"
		if codex {
			side, key = "codex", "mcp_servers"
		}
		before := &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(input)), Mode: 0600}
		doc, err := document(side, before)
		if err != nil {
			return
		}
		entries, ok := doc[key].(map[string]any)
		if !ok {
			return
		}
		server, ok := entries["demo"].(map[string]any)
		if !ok {
			return
		}
		server["command"] = "new"
		if got := preserveMCPText(side, before, doc); got != nil {
			actual, err := document(side, got)
			must(t, err)
			if actual[key].(map[string]any)["demo"].(map[string]any)["command"] != "new" {
				t.Fatal("patch lost desired value")
			}
		}
	})
}
