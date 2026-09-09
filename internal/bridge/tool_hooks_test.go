package bridge

import (
	"strings"
	"testing"
)

func TestBashHookDefinitionTranslation(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			f := hooksFixture(t)
			source := strings.ReplaceAll(strings.ReplaceAll(f.read("claude-source"), "SessionStart", event), "^startup$", "^Bash$")
			f.write("claude-source", source)
			f.apply()
			if !strings.Contains(f.read("codex-source"), event) {
				t.Fatal("tool event missing")
			}
			f.write("codex-source", strings.ReplaceAll(f.read("codex-source"), "/usr/bin/true", "/usr/bin/false"))
			_, err := Apply(f.c, Options{BeforeWrite: failSecond})
			contains(t, err, "rolled back")
			f.expect("claude-source", source)
			f.apply()
			if !strings.Contains(f.read("claude-source"), "/usr/bin/false") {
				t.Fatal("reverse event update missing")
			}
			before := f.read("state/manifest.json")
			f.apply()
			f.expect("state/manifest.json", before)
			for _, matcher := range []string{"Bash", "^Read$", "^Bash$|^Write$", ""} {
				f.write("codex-source", strings.ReplaceAll(f.read("claude-source"), "^Bash$", matcher))
				if _, err := Apply(f.c, Options{}); err == nil {
					t.Fatal("unsupported matcher accepted")
				}
			}
		})
	}
}
