package bridge

import (
	"strings"
	"testing"
)

const pluginHookFixture = `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/usr/bin/true","timeout":10}]}]}}`

func TestPluginHookTranslation(t *testing.T) {
	f := pluginFixture(t)
	f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	f.apply()
	if !strings.Contains(f.read("codex-plugin/hooks/hooks.json"), "UserPromptSubmit") {
		t.Fatal("hook missing")
	}
	f.write("codex-plugin/hooks/hooks.json", strings.Replace(f.read("codex-plugin/hooks/hooks.json"), "/usr/bin/true", "/usr/bin/false", 1))
	_, err := Apply(f.c, Options{BeforeWrite: failSecond})
	contains(t, err, "rolled back")
	f.apply()
	if !strings.Contains(f.read("claude-plugin/hooks/hooks.json"), "/usr/bin/false") {
		t.Fatal("reverse hook edit lost")
	}
	before := f.read("state/manifest.json")
	f.apply()
	f.expect("state/manifest.json", before)
	f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	f.write("codex-plugin/hooks/hooks.json", strings.Replace(pluginHookFixture, "/usr/bin/true", "/bin/true", 1))
	_, err = Apply(f.c, Options{})
	contains(t, err, "conflicts")
}

func TestPluginHooksRejectUnsupportedDefinitions(t *testing.T) {
	for _, raw := range []string{`{}`, strings.Replace(pluginHookFixture, "UserPromptSubmit", "PreToolUse", 1), strings.Replace(pluginHookFixture, "/usr/bin/true", "${CLAUDE_PLUGIN_ROOT}/hook", 1)} {
		f := pluginFixture(t)
		f.write("claude-plugin/hooks/hooks.json", raw)
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatal("unsupported package hook accepted")
		}
		f.missing("codex-plugin")
	}
}

func TestPortablePluginHooksFailClosed(t *testing.T) {
	f := pluginFixture(t)
	f.raw.Resources[0].CodexPluginLayout = "portable"
	f.load()
	f.write("claude-plugin/hooks/hooks.json", pluginHookFixture)
	_, err := Apply(f.c, Options{})
	contains(t, err, "does not load bundled hooks")
	f.missing("codex-plugin")
}
