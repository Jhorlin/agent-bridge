package bridge

import (
	"strings"
	"testing"
)

func TestAgentColorRemainsNativeLocal(t *testing.T) {
	f := agentSettingsFixture(t)
	f.write("claude-source", strings.Replace(f.read("claude-source"), "model: inherit", "model: inherit\ncolor: pink", 1))
	f.apply()
	if strings.Contains(f.read("codex-source"), "color") || strings.Contains(f.read("state/shared/portable"), "color") {
		t.Fatal("color crossed hosts")
	}
	f.write("codex-source", strings.Replace(f.read("codex-source"), "Review carefully.", "Reverse edit.", 1))
	f.apply()
	if !strings.Contains(f.read("claude-source"), "color: pink") || !strings.Contains(f.read("claude-source"), "Reverse edit.") {
		t.Fatal("color or reverse edit lost")
	}
}

func TestPluginAgentColorRemainsNativeLocal(t *testing.T) {
	f := pluginAgentFixture(t)
	f.raw.Resources[0].PreserveAgentSettings = true
	f.load()
	f.write("claude-plugin/agents/reviewer.md", strings.Replace(f.read("claude-plugin/agents/reviewer.md"), "name: reviewer", "name: reviewer\ncolor: cyan", 1))
	f.apply()
	path := "codex-home/agents/bridge-bundle-reviewer.toml"
	if strings.Contains(f.read(path), "color") {
		t.Fatal("plugin color crossed hosts")
	}
	f.write(path, strings.Replace(f.read(path), "Review the fixture only.", "Reverse edit.", 1))
	f.apply()
	if !strings.Contains(f.read("claude-plugin/agents/reviewer.md"), "color: cyan") {
		t.Fatal("plugin color lost")
	}
}

func TestAgentColorRejectsMalformedValues(t *testing.T) {
	for _, v := range []any{true, 42, "", strings.Repeat("x", 65), "red\nblue", "red\x00"} {
		if _, _, err := splitAgentSettings("claude", map[string]any{"color": v}); err == nil {
			t.Fatal("invalid color accepted")
		}
	}
	if _, _, err := splitAgentSettings("codex", map[string]any{"color": "pink"}); err == nil {
		t.Fatal("Codex accepted Claude setting")
	}
}
