package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginCopyCLI(t *testing.T) {
	dir, _ := setup(t)
	profile := filepath.Join(dir, "plugin.json")
	for name, content := range map[string]string{
		"plugin.json": `{"version":1,"stateDir":"plugin-state","resources":[{"id":"demo","kind":"plugin-directory","scope":"project","portable":true,"claude":"claude-plugin","codex":"codex-plugin"}]}`,
		"claude-plugin/.claude-plugin/plugin.json": `{"name":"demo","version":"1.0.0"}`,
		"claude-plugin/skills/demo/SKILL.md":       "---\nname: demo\ndescription: Fixture.\n---\nInert fixture.\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"sync", profile}, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	for _, name := range []string{".codex-plugin/plugin.json", "skills/demo/SKILL.md"} {
		data, err := os.ReadFile(filepath.Join(dir, "codex-plugin", name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "copy", name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"compare-plugin-copy", profile, "demo", "codex", filepath.Join(dir, "copy")}
	if code := Run(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	if err := os.WriteFile(filepath.Join(dir, "copy/skills/demo/SKILL.md"), []byte("old copy"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), args, &out, &errOut); code != 2 {
		t.Fatal(code, &errOut)
	}
	if code := Run(context.Background(), args, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
	if code := Run(context.Background(), args[:2], &out, &errOut); code != 1 {
		t.Fatal(code)
	}
}
