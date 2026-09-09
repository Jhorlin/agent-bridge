package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func TestFileChangeCLI(t *testing.T) {
	dir, profile := setup(t)
	profile = filepath.Join(dir, "supporting.json")
	for name, content := range map[string]string{
		"supporting.json":                  `{"version":1,"stateDir":"supporting-state","resources":[{"id":"demo","kind":"skill-directory","scope":"project","portable":true,"claude":"claude-skill","codex":"codex-skill"}]}`,
		"claude-skill/SKILL.md":            "---\nname: demo\ndescription: Fixture.\n---\nRead references/test.txt.\n",
		"claude-skill/references/test.txt": "PRIVATE-FIXTURE-CONTENT",
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
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return Run(context.Background(), args, &out, &errOut)
	}
	if code := run("sync", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("review-file-change", profile, "demo/references/test.txt", "--rename", "references/renamed.txt"); code != 0 {
		t.Fatal(code, &errOut)
	}
	if strings.Contains(out.String(), "PRIVATE-FIXTURE-CONTENT") {
		t.Fatal("review exposed private content")
	}
	var review bridge.FileChangeReview
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if code := run("apply-file-change", profile, review.Observation, "demo/references/test.txt", "--delete"); code != 2 {
		t.Fatal(code, &errOut)
	}
	if code := run("apply-file-change", profile, review.Observation, "demo/references/test.txt", "--rename", "references/renamed.txt"); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("sync", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("review-file-change", profile, "demo/references/renamed.txt", "--delete"); code != 0 {
		t.Fatal(code, &errOut)
	}
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if code := run("apply-file-change", profile, review.Observation, "demo/references/renamed.txt", "--delete"); code != 0 {
		t.Fatal(code, &errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "codex-skill/references/renamed.txt")); !os.IsNotExist(err) {
		t.Fatal("file not deleted", err)
	}
	if code := run("recover-file-change", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	for _, args := range [][]string{{"review-file-change"}, {"apply-file-change", profile}, {"recover-file-change", profile, "extra"}, {"review-file-change", profile, "demo/references/x", "--rename", ""}, {"review-file-change", profile, "demo/references/x", "--delete", "extra"}} {
		if code := run(args...); code != 1 {
			t.Fatal(code, args)
		}
	}
}
