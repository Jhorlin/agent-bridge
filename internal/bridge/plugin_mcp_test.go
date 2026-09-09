package bridge

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func pluginMCPFixture(t *testing.T) *fixture {
	f := pluginFixture(t)
	f.raw.Resources[0].Servers = []string{"docs"}
	f.raw.Resources[0].AllowReformat = true
	f.load()
	f.write("claude-plugin/.mcp.json", `{"mcpServers":{"docs":{"command":"demo-server","env":{"TOKEN":"${TOKEN}"}}}}`)
	return f
}

func TestPluginMCPRoundTripConflictHistoryAndRollback(t *testing.T) {
	f := pluginMCPFixture(t)
	tx := initialHistory(t, f)
	if !strings.Contains(f.read("codex-plugin/.mcp.json"), "${TOKEN}") {
		t.Fatal("lost variable reference")
	}
	f.write("codex-plugin/.mcp.json", strings.Replace(f.read("codex-plugin/.mcp.json"), "demo-server", "reverse-server", 1))
	before := f.read("claude-plugin/.mcp.json")
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.expect("claude-plugin/.mcp.json", before)
	f.apply()
	if !strings.Contains(f.read("claude-plugin/.mcp.json"), "reverse-server") {
		t.Fatal("reverse edit missing")
	}
	tree := auditTree(t, f.dir)
	f.apply()
	if !reflect.DeepEqual(tree, auditTree(t, f.dir)) {
		t.Fatal("not idempotent")
	}
	f.write("claude-plugin/.mcp.json", strings.Replace(f.read("claude-plugin/.mcp.json"), "reverse-server", "chosen-server", 1))
	f.write("codex-plugin/.mcp.json", strings.Replace(f.read("codex-plugin/.mcp.json"), "reverse-server", "conflicting-server", 1))
	choices := map[string]string{"bundle/.mcp.json": "claude"}
	r, err := ReviewResolution(f.path("config.json"), choices)
	must(t, err)
	_, err = ResolveReviewed(f.path("config.json"), r.Observation, choices)
	must(t, err)
	if !strings.Contains(f.read("codex-plugin/.mcp.json"), "chosen-server") {
		t.Fatal("resolution failed")
	}
	history := HistoryChoice{tx, "bundle/.mcp.json", "codex", "after"}
	r, err = ReviewHistory(f.path("config.json"), history)
	must(t, err)
	_, err = RestoreReviewed(f.path("config.json"), r.Observation, history)
	must(t, err)
	if !strings.Contains(f.read("codex-plugin/.mcp.json"), "demo-server") {
		t.Fatal("history failed")
	}
	must(t, os.Remove(f.path("codex-plugin/.mcp.json")))
	if !f.plan().HasConflicts() {
		t.Fatal("deletion not blocked")
	}
}

func TestPluginMCPRejectsUnsafeOrUnselectedData(t *testing.T) {
	for _, body := range []string{
		`{"mcpServers":{"docs":{"command":"${CLAUDE_PLUGIN_ROOT}/script"}}}`,
		`{"mcpServers":{"docs":{"command":"demo","env":{"TOKEN":"literal"}}}}`,
		`{"mcpServers":{"docs":{"command":"demo"},"extra":{"command":"extra"}}}`,
		`{"mcpServers":{}}`, `{"mcpServers":{"docs":{"command":"demo"}},"unknown":true}`,
	} {
		f := pluginMCPFixture(t)
		f.write("claude-plugin/.mcp.json", body)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsafe package accepted")
		}
		f.missing("codex-plugin")
	}
	for _, mode := range []string{"no-allowlist", "no-reformat", "portable-layout", "missing-component"} {
		f := pluginMCPFixture(t)
		switch mode {
		case "no-allowlist":
			f.raw.Resources[0].Servers = nil
			f.raw.Resources[0].AllowReformat = false
		case "no-reformat":
			f.raw.Resources[0].AllowReformat = false
		case "portable-layout":
			f.raw.Resources[0].CodexPluginLayout = "portable"
		case "missing-component":
			must(t, os.Remove(f.path("claude-plugin/.mcp.json")))
		}
		f.save()
		c, err := LoadAuditConfig(f.path("config.json"))
		if err == nil {
			_, err = Apply(c, Options{})
		}
		if err == nil {
			t.Fatal("missing consent accepted", mode)
		}
		f.missing("codex-plugin")
	}
}
