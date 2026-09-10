package bridge

import (
	"strings"
	"testing"
)

func TestPluginMCPDirectMapRoundTrip(t *testing.T) {
	f := pluginMCPFixture(t)
	f.write("claude-plugin/.mcp.json", `{"docs":{"command":"demo-server","env":{"TOKEN":"${TOKEN}"}}}`)
	f.apply()
	if !strings.Contains(f.read("codex-plugin/.mcp.json"), "demo-server") {
		t.Fatal("missing direct-map server")
	}
	f.write("codex-plugin/.mcp.json", strings.ReplaceAll(f.read("codex-plugin/.mcp.json"), "demo-server", "reverse-server"))
	f.apply()
	got := f.read("claude-plugin/.mcp.json")
	if strings.Contains(got, "mcpServers") || !strings.Contains(got, "reverse-server") {
		t.Fatal("lost direct-map layout or reverse edit")
	}
}

func TestPluginConventionsDiscoverDirectMCP(t *testing.T) {
	f := newFixture(t)
	f.write("config.json", `{"version":1,"stateDir":"state","conventions":{"root":".","scope":"global","features":["plugins"]},"resources":[]}`)
	f.write(".agent-bridge-plugins/claude/demo/.claude-plugin/plugin.json", `{"name":"demo"}`)
	f.write(".agent-bridge-plugins/claude/demo/.mcp.json", `{"docs":{"command":"demo-server"}}`)
	reloadConventions(t, f)
	if len(f.c.Resources) != 1 || len(f.c.Resources[0].Servers) != 1 || f.c.Resources[0].Servers[0] != "docs" {
		t.Fatal("direct-map server not discovered")
	}
	f.apply()
}

func TestPluginMCPDirectMapRejectsUnsafeData(t *testing.T) {
	for _, body := range []string{
		`{"docs":{"command":"demo","args":["--password","PRIVATE_FIXTURE_SECRET"]}}`,
		`{"docs":{"command":"demo"},"extra":{"command":"extra"}}`,
		`{"docs":false}`, `{"docs":{"command":"demo"},"unknown":true}`,
		`{"mcpServers":{"docs":{"command":"demo"}},"docs":{"command":"demo"}}`,
	} {
		f := pluginMCPFixture(t)
		f.write("claude-plugin/.mcp.json", body)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsafe direct map accepted")
		}
		f.missing("codex-plugin")
	}
}
