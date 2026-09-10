package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventorySkillsCLI(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"inventory-skills", root}, &out, &errs); code != 0 || !strings.Contains(out.String(), `"readOnly":true`) {
		t.Fatalf("%d %s %s", code, &out, &errs)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("inventory wrote state")
	}
	for _, args := range [][]string{{"inventory-skills"}, {"inventory-skills", root, "--apply"}, {"inventory-skills", root, "--global"}, {"inventory-skills", filepath.Join(root, "missing")}} {
		if Run(context.Background(), args, &out, &errs) != 1 {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude/skills/broken"), 0700); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"inventory-skills", root, "--include-plugin-cache"}, &out, &errs); code != 2 {
		t.Fatalf("blocked exit %d", code)
	}
	if code := Run(context.Background(), []string{"inventory-skills", root}, &auditFailWriter{}, &errs); code != 1 {
		t.Fatal("ignored output failure")
	}
}
