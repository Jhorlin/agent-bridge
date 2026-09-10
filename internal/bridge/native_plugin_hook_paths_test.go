package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestNativePluginRelativeHookExecution(t *testing.T) {
	nativePluginRelativeHookExecution(t, "")
}

func TestNativePluginInterpretedHookExecution(t *testing.T) {
	for _, interpreter := range []string{"sh", "bash"} {
		t.Run(interpreter, func(t *testing.T) { nativePluginRelativeHookExecution(t, interpreter) })
	}
}

func nativePluginRelativeHookExecution(t *testing.T, interpreter string) {
	f := pluginFixture(t)
	manifest := f.read("claude-plugin/.claude-plugin/plugin.json")
	skill := f.read("claude-plugin/skills/demo/SKILL.md")
	f.dir = f.path("fixture with spaces")
	tools := nativeTools(t, f)
	f.raw.Resources[0].Codex = "home/plugins/demo"
	f.load()
	f.write("claude-plugin/.claude-plugin/plugin.json", manifest)
	f.write("claude-plugin/skills/demo/SKILL.md", skill)
	f.write("claude-plugin/hooks/run.sh", "#!/bin/sh\nprintf '%s\\n' \"$CLAUDE_PLUGIN_ROOT\" > '"+f.path("hook.log")+"'\npwd >> '"+f.path("hook.log")+"'\n")
	must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0755))
	f.write("claude-plugin/hooks/hooks.json", `{"hooks":{"SessionStart":[{"matcher":"^startup$","hooks":[{"type":"command","command":"\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\"","timeout":10}]}]}}`)
	if interpreter != "" {
		f.write("claude-plugin/hooks/input.txt", "inert-argument-marker\n")
		f.write("claude-plugin/hooks/run.sh", f.read("claude-plugin/hooks/run.sh")+"cat \"$1\" >> '"+f.path("hook.log")+"'\n")
		must(t, os.Chmod(f.path("claude-plugin/hooks/run.sh"), 0600))
		command, err := json.Marshal(interpreter + ` "${CLAUDE_PLUGIN_ROOT}/hooks/run.sh" "${CLAUDE_PLUGIN_ROOT}/hooks/input.txt"`)
		must(t, err)
		f.write("claude-plugin/hooks/hooks.json", strings.Replace(f.read("claude-plugin/hooks/hooks.json"), `"\"${CLAUDE_PLUGIN_ROOT}/hooks/run.sh\""`, string(command), 1))
	}
	f.apply()
	// Execute the reverse-generated script in Claude, not only the source.
	f.write("home/plugins/demo/hooks/run.sh", f.read("home/plugins/demo/hooks/run.sh")+"# Reverse-synchronized fixture\n")
	if interpreter == "" {
		must(t, os.Chmod(f.path("home/plugins/demo/hooks/run.sh"), 0755))
	}
	f.apply()
	f.expect("claude-plugin/hooks/run.sh", f.read("home/plugins/demo/hooks/run.sh"))
	server, _ := nativePluginFixtureProvider(t)
	f.write(".claude-plugin/marketplace.json", `{"name":"bridge-fixture","owner":{"name":"Bridge tests"},"plugins":[{"name":"demo","source":"./claude-plugin"}]}`)
	nativeRun(t, f, tools["claude"], "plugin", "marketplace", "add", f.dir)
	nativeRun(t, f, tools["claude"], "plugin", "install", "demo@bridge-fixture", "--scope", "user")
	nativeRunEnvironment(t, f, tools["claude"], []string{"ANTHROPIC_BASE_URL=" + server.URL, "ANTHROPIC_API_KEY=bridge-fixture-not-a-real-key"}, "--print", "--model", "sonnet", "--max-turns", "1", "--no-session-persistence", "--setting-sources", "user", "Return fixture complete.")
	// A local Claude marketplace may execute from its authoring root rather
	// than the installPath shown by plugin list. Let the native token resolve it.
	argumentOutput := ""
	if interpreter != "" {
		argumentOutput = "inert-argument-marker\n"
	}
	f.expect("hook.log", f.path("claude-plugin")+"\n"+f.dir+"\n"+argumentOutput)
	must(t, os.Remove(f.path("hook.log")))
	f.write("home/.agents/plugins/marketplace.json", `{"name":"personal","interface":{"displayName":"Personal"},"plugins":[{"name":"demo","source":{"source":"local","path":"./plugins/demo"},"policy":{"installation":"AVAILABLE","authentication":"ON_INSTALL"},"category":"Productivity"}]}`)
	nativeRun(t, f, tools["codex"], "plugin", "marketplace", "add", f.path("home"), "--json")
	nativeRun(t, f, tools["codex"], "plugin", "add", "demo@personal", "--json")
	f.write("codex-home/config.toml", nativePluginProviderConfig(server.URL)+"\n"+f.read("codex-home/config.toml"))
	hooks := nativeRPC(t, f, tools["codex"], "hooks/list", map[string]any{"cwds": []string{f.dir}})
	var parsed map[string]any
	must(t, json.Unmarshal(hooks, &parsed))
	if !strings.Contains(string(hooks), `"untrusted"`) {
		t.Fatal("native plugin hook was not discovered untrusted")
	}
	nativeRun(t, f, tools["codex"], "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
	f.missing("hook.log")
	// Invocation-only consent for this inert temp-home fixture, never persisted
	// or exposed by the synchronizer. Production trust is entirely native-managed.
	nativeRun(t, f, tools["codex"], "exec", "--dangerously-bypass-hook-trust", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "Return fixture complete.")
	f.expect("hook.log", f.path("codex-home/plugins/cache/personal/demo/1.0.0")+"\n"+f.dir+"\n"+argumentOutput)
}
