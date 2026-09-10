package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginCandidatesCLIReadOnlyAndRedacted(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"left/.claude-plugin/plugin.json": `{"name":"PRIVATE_NAME","version":"PRIVATE_VERSION","repository":"PRIVATE_REPO","author":{"name":"PRIVATE_AUTHOR"}}`,
		"right/.codex-plugin/plugin.json": `{"name":"PRIVATE_NAME","version":"different","repository":"PRIVATE_REPO","author":{"name":"OTHER_AUTHOR"},"apps":{"PRIVATE_APP":"PRIVATE_SECRET"}}`,
		"left/skills/shared/SKILL.md":     "PRIVATE_BODY",
		"right/skills/shared/SKILL.md":    "OTHER_BODY",
		"right/skills/extra/SKILL.md":     "PRIVATE_BODY",
	} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	args := []string{"compare-plugin-candidates", filepath.Join(root, "left"), filepath.Join(root, "right")}
	if code := RunLogged(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatalf("candidate inspection should complete without a profile: code=%d error=%s", code, &errOut)
	}
	var report struct {
		ReadOnly     bool                                                                   `json:"readOnly"`
		Status       string                                                                 `json:"status"`
		Comparisons  []struct{ Name, Version, Repository, Author string }                   `json:"comparisons"`
		Capabilities map[string]struct{ Left, Right, CommonPaths, LeftOnly, RightOnly int } `json:"capabilities"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.ReadOnly || report.Status != "review-required" || len(report.Comparisons) != 1 {
		t.Fatalf("invalid inspection report: %s", &out)
	}
	comparison := report.Comparisons[0]
	if comparison.Name != "match" || comparison.Version != "mismatch" || comparison.Repository != "match" || comparison.Author != "mismatch" {
		t.Fatalf("wrong identity comparison: %+v", comparison)
	}
	if got := report.Capabilities["skills"]; got.Left != 1 || got.Right != 2 || got.CommonPaths != 1 || got.LeftOnly != 0 || got.RightOnly != 1 {
		t.Fatalf("wrong conventional skill counts: %+v", got)
	}
	if strings.Contains(out.String()+errOut.String(), "PRIVATE") || strings.Contains(out.String(), root) || strings.Contains(out.String(), "OTHER_AUTHOR") {
		t.Fatal("candidate output leaked input values")
	}
	if command, _ := loggedCommand(args); command != "" {
		t.Fatal("read-only command would create diagnostic logs")
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), args[:2], &out, &errOut); code != 1 {
		t.Fatal("wrong arity accepted")
	}
	errOut.Reset()
	if code := Run(context.Background(), args, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal("output failure ignored")
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), []string{args[0], "PRIVATE_RELATIVE", args[2]}, &out, &errOut); code != 1 || strings.Contains(errOut.String(), "PRIVATE") || out.Len() != 0 {
		t.Fatal("unsafe root not rejected privately")
	}
}
