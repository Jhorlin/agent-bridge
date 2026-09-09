package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPluginCopyComparison(t *testing.T) {
	for _, mode := range []string{"same", "changed", "missing", "unexpected", "generated", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			f := pluginFixture(t)
			f.apply()
			r := f.c.Resources[0]
			files, err := walk(r.Paths["codex"])
			must(t, err)
			for _, name := range files {
				s, err := snapshot(filepath.Join(r.Paths["codex"], name))
				must(t, err)
				must(t, writeSnapshot(f.path("copy/"+name), s))
			}
			switch mode {
			case "changed":
				f.write("copy/skills/demo/SKILL.md", "PRIVATE-CONTENT")
			case "missing":
				must(t, os.Remove(f.path("copy/skills/demo/SKILL.md")))
			case "unexpected":
				f.write("copy/local-secret.json", "PRIVATE-CONTENT")
			case "generated":
				f.write("copy/.codex-plugin/migrated-command-skills/demo/SKILL.md", "host-generated")
			case "symlink":
				must(t, os.Symlink(f.path("copy/skills/demo/SKILL.md"), f.path("copy/link")))
			}
			before := auditTree(t, f.dir)
			report, err := ComparePluginCopy(f.path("config.json"), r.ID, "codex", f.path("copy"))
			if mode == "symlink" {
				if err == nil {
					t.Fatal("symlink copy accepted")
				}
				return
			}
			must(t, err)
			if report.Matches != (mode == "same" || mode == "generated") {
				t.Fatal(mode, report)
			}
			if !reflect.DeepEqual(before, auditTree(t, f.dir)) {
				t.Fatal("comparison wrote files")
			}
			data, err := json.Marshal(report)
			must(t, err)
			if strings.Contains(string(data), "PRIVATE-CONTENT") {
				t.Fatal("private file content exposed")
			}
			if report.Matches && report.SourceDigest != report.CopyDigest {
				t.Fatal("matching package digests differ")
			}
		})
	}
}

func TestPluginCopyRefusesInvalidTargets(t *testing.T) {
	f := pluginFixture(t)
	f.apply()
	r := f.c.Resources[0]
	for _, target := range []string{"relative", f.path("missing"), r.Paths["codex"], f.dir} {
		if _, err := ComparePluginCopy(f.path("config.json"), r.ID, "codex", target); err == nil {
			t.Fatal("unsafe copy target accepted")
		}
	}
	if _, err := ComparePluginCopy(f.path("config.json"), r.ID, "shared", f.path("copy")); err == nil {
		t.Fatal("shared side accepted")
	}
}
