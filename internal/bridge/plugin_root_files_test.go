package bridge

import (
	"os"
	"testing"
)

func TestPluginRootSupportingFiles(t *testing.T) {
	f := pluginFixture(t)
	files := map[string]string{"PRIVACY.md": "Fixture privacy notice.", "CHANGELOG.md": "Fixture changes.", "LICENSE.txt": "Fixture license.", "NOTICE": "Fixture notice.", "demo-example.png": "inert binary fixture", "demo.jpg": "jpeg fixture", "demo.jpeg": "jpeg fixture", "demo.webp": "webp fixture", "demo.svg": "<svg/>", "Demo Logo (Light).svg": "<svg/>"}
	for name, body := range files {
		f.write("claude-plugin/"+name, body)
	}
	f.apply()
	for name, body := range files {
		f.expect("codex-plugin/"+name, body)
	}
	f.write("codex-plugin/PRIVACY.md", "Updated fixture notice.")
	f.apply()
	f.expect("claude-plugin/PRIVACY.md", "Updated fixture notice.")
	for _, item := range f.plan().Summaries() {
		if item.Status != "in-sync" {
			t.Fatal("root supporting files did not converge")
		}
	}
}

func TestPluginEmptySkillPlaceholder(t *testing.T) {
	f := pluginFixture(t)
	f.write("claude-plugin/skills/.gitkeep", "")
	f.apply()
	f.expect("codex-plugin/skills/.gitkeep", "")
	f.write("codex-plugin/skills/.gitkeep", "not an empty placeholder")
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("nonempty marker accepted")
	}
	f.write("codex-plugin/skills/.gitkeep", "")
	must(t, os.Chmod(f.path("codex-plugin/skills/.gitkeep"), 0755))
	if _, err := Apply(f.c, Options{}); err == nil {
		t.Fatal("executable marker accepted")
	}
}

func TestPluginRootSupportingFilesDoNotAdoptConfiguration(t *testing.T) {
	for _, name := range []string{".env", ".gitignore", "settings.json", "other-plugin.json", ".codex-plugin/plugin.json", "package.json", "run.sh", "subdir/example.png", "PRIVACY.md/private.json"} {
		t.Run(name, func(t *testing.T) {
			f := pluginFixture(t)
			f.write("claude-plugin/"+name, "{}")
			if _, err := Plan(f.c); err == nil {
				t.Fatal("unknown package configuration or layout was adopted")
			}
		})
	}
	for _, name := range []string{"PRIVACY.md", "example.png"} {
		f := pluginFixture(t)
		f.write("claude-plugin/"+name, "fixture")
		must(t, os.Chmod(f.path("claude-plugin/"+name), 0755))
		if _, err := Plan(f.c); err == nil {
			t.Fatal("executable root file accepted as documentation")
		}
	}
}
