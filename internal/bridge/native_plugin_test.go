package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeClaudePluginInstallAndRemove(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	f.apply()
	// Exercise the reverse-generated Claude package, not just its initial source.
	f.write("codex-plugin/.codex-plugin/plugin.json", strings.Replace(f.read("codex-plugin/.codex-plugin/plugin.json"), "1.0.0", "1.1.0", 1))
	f.apply()
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "add", f.dir)
	nativeRun(t, f, tools["claude"], "plugin", "install", "demo@bridge-fixture", "--scope", "user")
	output := nativeRun(t, f, tools["claude"], "plugin", "list", "--json")
	if !strings.Contains(output, "demo@bridge-fixture") || !strings.Contains(output, "1.1.0") {
		t.Fatalf("reverse-generated plugin not installed: %s", output)
	}
	nativeRun(t, f, tools["claude"], "plugin", "uninstall", "demo@bridge-fixture", "--scope", "user")
	output = nativeRun(t, f, tools["claude"], "plugin", "list", "--json")
	if strings.Contains(output, "demo@bridge-fixture") {
		t.Fatalf("plugin remains installed: %s", output)
	}
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "remove", "bridge-fixture")
	f.expect("claude-plugin/skills/demo/SKILL.md", f.read("codex-plugin/skills/demo/SKILL.md"))
}

func TestNativeCodexPluginInstallAndRemove(t *testing.T) {
	for _, layout := range []string{"compatibility", "portable"} {
		t.Run(layout, func(t *testing.T) { nativeCodexPluginLifecycle(t, layout) })
	}
}

func nativeCodexPluginLifecycle(t *testing.T, layout string) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	if layout == "portable" {
		f.raw.Resources[0].CodexPluginLayout = "portable"
	}
	f.load()
	if layout != "portable" {
		f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	}
	f.apply()
	// Only a disposable personal marketplace is created; no user cache is read.
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	compare := func(want bool) {
		t.Helper()
		report, err := ComparePluginCopy(f.path("config.json"), "bundle", "codex", f.path("codex-home/plugins/cache/personal/demo/1.0.0"))
		must(t, err)
		if report.Matches != want {
			t.Fatalf("unexpected native cache comparison: %+v", report)
		}
	}
	nativeRPCSession(t, f, tools["codex"], func(call func(string, any) json.RawMessage) {
		call("plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
		if layout != "portable" {
			hooks := call("hooks/list", map[string]any{"cwds": []string{f.dir}})
			if !strings.Contains(string(hooks), "userPromptSubmit") || !strings.Contains(string(hooks), "untrusted") {
				t.Fatalf("plugin hook not discovered untrusted: %s", hooks)
			}
		}
		skills := call("skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
		if !strings.Contains(string(skills), "demo") || !strings.Contains(string(skills), "plugins/cache") {
			t.Fatalf("installed plugin skill not discovered: %s", skills)
		}
		compare(true)
		f.write("claude-plugin/skills/demo/SKILL.md", "---\nname: demo\ndescription: Updated bridge reload fixture.\n---\nRead the updated docs.")
		f.apply()
		skills = call("skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
		if strings.Contains(string(skills), "Updated bridge reload fixture") {
			t.Fatal("installed copy unexpectedly changed with authoring source")
		}
		compare(false)
		call("plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
		skills = call("skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
		if strings.Contains(string(skills), "plugins/cache/personal/demo") {
			t.Fatalf("removed plugin still active: %s", skills)
		}
		call("plugin/install", map[string]any{"marketplacePath": f.path("home/.agents/plugins/marketplace.json"), "pluginName": "demo"})
		skills = call("skills/list", map[string]any{"cwds": []string{f.dir}, "forceReload": true})
		if !strings.Contains(string(skills), "Updated bridge reload fixture") {
			t.Fatalf("reinstalled plugin did not refresh: %s", skills)
		}
		compare(true)
		call("plugin/uninstall", map[string]any{"pluginId": "demo@personal"})
	})
	f.expect("home/plugins/demo/skills/demo/SKILL.md", f.read("claude-plugin/skills/demo/SKILL.md"))
}
