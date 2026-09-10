package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// These are native contract tests, not a production refresh implementation.
// They record the incompatible cache/enablement behavior that orchestration
// must respect. Every host process uses disposable native homes.
func TestNativeClaudePluginRefreshContract(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.apply()
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	run := func(args ...string) string { return nativeRun(t, f, tools["claude"], args...) }
	run("plugin", "marketplace", "add", f.dir)
	run("plugin", "install", "demo@bridge-fixture", "--scope", "user")
	run("plugin", "disable", "demo@bridge-fixture", "--scope", "user")
	settings := f.read("claude-home/settings.json")
	old := f.read("claude-home/plugins/cache/bridge-fixture/demo/1.0.0/skills/demo/SKILL.md")
	updated := "---\nname: demo\ndescription: Native refresh fixture.\n---\nUpdated instruction.\n"
	f.write("claude-plugin/skills/demo/SKILL.md", updated)
	run("plugin", "marketplace", "update", "bridge-fixture")
	run("plugin", "update", "demo@bridge-fixture", "--scope", "user")
	f.expect("claude-home/plugins/cache/bridge-fixture/demo/1.0.0/skills/demo/SKILL.md", old)
	f.expect("claude-home/settings.json", settings)
	f.write("claude-plugin/.claude-plugin/plugin.json", strings.Replace(f.read("claude-plugin/.claude-plugin/plugin.json"), "1.0.0", "1.1.0", 1))
	run("plugin", "marketplace", "update", "bridge-fixture")
	run("plugin", "update", "demo@bridge-fixture", "--scope", "user")
	f.expect("claude-home/plugins/cache/bridge-fixture/demo/1.1.0/skills/demo/SKILL.md", updated)
	f.expect("claude-home/settings.json", settings)
	var installed []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	}
	must(t, json.Unmarshal([]byte(run("plugin", "list", "--json")), &installed))
	if len(installed) != 1 || installed[0].ID != "demo@bridge-fixture" || installed[0].Version != "1.1.0" || installed[0].Enabled {
		t.Fatal("native Claude refresh no longer preserves disabled versioned installation")
	}
}

func TestNativeCodexPluginRefreshContract(t *testing.T) {
	f := pluginFixture(t)
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	f.load()
	f.apply()
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	run := func(args ...string) string { return nativeRun(t, f, tools["codex"], args...) }
	run("plugin", "marketplace", "add", f.path("home"), "--json")
	run("plugin", "add", "demo@personal", "--json")
	updated := "---\nname: demo\ndescription: Native refresh fixture.\n---\nUpdated instruction.\n"
	f.write("home/plugins/demo/skills/demo/SKILL.md", updated)
	for _, version := range []string{"1.0.0", "1.1.0"} {
		if version == "1.1.0" {
			f.write("home/plugins/demo/.codex-plugin/plugin.json", strings.Replace(f.read("home/plugins/demo/.codex-plugin/plugin.json"), "1.0.0", version, 1))
		}
		config := f.read("codex-home/config.toml")
		if !strings.Contains(config, "enabled = true") {
			t.Fatal("fixture plugin was not enabled after native install")
		}
		f.write("codex-home/config.toml", strings.ReplaceAll(config, "enabled = true", "enabled = false"))
		// Even an explicit process override does not preserve the persisted
		// disabled choice. Do not use native add as an automatic refresh.
		run("-c", `plugins."demo@personal".enabled=false`, "plugin", "add", "demo@personal", "--json")
		f.expect("codex-home/plugins/cache/personal/demo/"+version+"/skills/demo/SKILL.md", updated)
		if !strings.Contains(f.read("codex-home/config.toml"), "enabled = true") {
			t.Fatal("native contract changed: check whether refresh can now preserve disabled state")
		}
		var result struct {
			Installed []struct {
				ID      string `json:"pluginId"`
				Version string `json:"version"`
				Enabled bool   `json:"enabled"`
			} `json:"installed"`
		}
		must(t, json.Unmarshal([]byte(run("plugin", "list", "--json", "--marketplace", "personal")), &result))
		if len(result.Installed) != 1 || result.Installed[0].ID != "demo@personal" || result.Installed[0].Version != version || !result.Installed[0].Enabled {
			t.Fatal("native Codex refresh contract changed")
		}
	}
}
