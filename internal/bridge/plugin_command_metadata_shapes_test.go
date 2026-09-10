package bridge

import (
	"reflect"
	"strings"
	"testing"
)

func TestPluginCommandNativeMetadataShapes(t *testing.T) {
	f := commandSettingsFixture(t)
	local := strings.Replace(commandFixture, "description:", "argument-hint: [project-name]\ndisable-model-invocation: false\ndescription:", 1)
	f.write("claude-plugin/commands/demo.md", local)
	f.apply()
	f.expect("claude-plugin/commands/demo.md", local)
	for _, path := range []string{"codex-plugin/commands/demo.md", "state/shared/" + f.c.Resources[0].ID + "/commands/demo.md"} {
		if raw := f.read(path); strings.Contains(raw, "argument-hint") || strings.Contains(raw, "disable-model-invocation") {
			t.Fatal("native display/default settings were exported")
		}
	}
	f.write("codex-plugin/commands/demo.md", strings.Replace(f.read("codex-plugin/commands/demo.md"), "fixed word", "reverse word", 1))
	f.apply()
	raw, err := snapshot(f.path("claude-plugin/commands/demo.md"))
	must(t, err)
	fields, body, err := agentDocument("claude", raw)
	must(t, err)
	if !reflect.DeepEqual(fields["argument-hint"], []any{"project-name"}) || fields["disable-model-invocation"] != false || !strings.Contains(body, "reverse word") {
		t.Fatal("reverse sync lost local YAML shape or portable changes")
	}
	for _, item := range f.plan().Items {
		if len(item.Writes) != 0 {
			t.Fatal("retained metadata did not converge")
		}
	}
}

func TestPluginCommandNativeMetadataShapesRejectPolicyAndMalformedHints(t *testing.T) {
	for _, field := range []string{"argument-hint: [12]", "argument-hint: []", "argument-hint: [true]", "argument-hint: {secret: value}", "disable-model-invocation: true", "disable-model-invocation: 'false'"} {
		f := commandSettingsFixture(t)
		f.write("claude-plugin/commands/demo.md", strings.Replace(commandFixture, "description:", field+"\ndescription:", 1))
		if _, err := Apply(f.c, Options{}); err == nil {
			t.Fatalf("accepted %s", field)
		}
		f.missing("codex-plugin/commands/demo.md")
	}
}
