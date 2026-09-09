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

func TestResolutionCLI(t *testing.T) {
	dir, profile := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("PRIVATE-CONFLICT"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"review-resolution", profile, "rules=claude"}, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	if strings.Contains(out.String(), "PRIVATE-CONFLICT") {
		t.Fatal("leaked content")
	}
	var review bridge.ReviewCheckpoint
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"review-resolution"}, {"resolve-reviewed", profile}, {"review-resolution", profile, "rules=claude", "rules=codex"}, {"review-resolution", profile, "rules"}, {"review-resolution", profile, "rules="}} {
		if code := Run(context.Background(), args, &out, &errOut); code != 1 {
			t.Fatal(code, args)
		}
	}
	if code := Run(context.Background(), []string{"resolve-reviewed", profile, review.Observation, "rules=codex"}, &out, &errOut); code != 2 {
		t.Fatal(code, &errOut)
	}
	if code := Run(context.Background(), []string{"review-resolution", profile, "rules=claude"}, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
	if code := Run(context.Background(), []string{"resolve-reviewed", profile, review.Observation, "rules=claude"}, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || string(data) != "one" {
		t.Fatal(string(data), err)
	}
}
