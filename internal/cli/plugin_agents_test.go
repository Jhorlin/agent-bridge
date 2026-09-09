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

func TestPluginAgentExportCLI(t *testing.T) {
	dir, _ := setup(t)
	profile := filepath.Join(dir, "plugin-export.json")
	for name, content := range map[string]string{
		"plugin-export.json":                       `{"version":1,"stateDir":"export-state","resources":[{"id":"demo","kind":"plugin-directory","scope":"global","portable":true,"allowReformat":true,"claude":"claude-plugin","codex":"codex-plugin","codexAgentExports":{"reviewer":"agents/bridge-demo-reviewer.toml"}}]}`,
		"claude-plugin/.claude-plugin/plugin.json": `{"name":"demo","version":"1.0.0"}`,
		"claude-plugin/agents/reviewer.md":         "---\nname: reviewer\ndescription: PRIVATE_AGENT_DESCRIPTION\n---\nPRIVATE_AGENT_BODY\n",
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
	for _, cmd := range []string{"audit", "plan", "review-profile"} {
		if code := run(cmd, profile); code != 0 {
			t.Fatal(code, &errOut)
		}
		if strings.Contains(out.String(), "PRIVATE_AGENT") {
			t.Fatal("output leaked agent contents")
		}
	}
	var review bridge.ReviewCheckpoint
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if code := run("sync-reviewed", profile, review.Observation); code != 0 {
		t.Fatal(code, &errOut)
	}
	export := filepath.Join(dir, "agents/bridge-demo-reviewer.toml")
	data, err := os.ReadFile(export)
	if err != nil || !strings.Contains(string(data), "bridge-demo-reviewer") {
		t.Fatal("standalone export missing", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "codex-plugin/agents/reviewer.md")); !os.IsNotExist(err) {
		t.Fatal("wrote unsupported Codex package agent", err)
	}
	if code := run("review-profile", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(export, append(data, []byte("# later formatting\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run("sync-reviewed", profile, review.Observation); code != 2 {
		t.Fatal(code, &errOut)
	}
}
