package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func TestEnrollmentCreationCLI(t *testing.T) {
	dir, template := setup(t)
	data, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["coordinationDir"] = "coordination"
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, data, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "registered.json")
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"review-enrollment", template, target}, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	var review bridge.ReviewCheckpoint
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"create-enrolled", template, target, review.Observation}, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("enrollment synchronized native data")
	}
	for _, args := range [][]string{{"review-enrollment"}, {"create-enrolled", template, target}, {"recover-enrollment", target}} {
		if code := Run(context.Background(), args, &out, &errOut); code != 1 {
			t.Fatal(code)
		}
	}
	if code := Run(context.Background(), []string{"recover-enrollment", target, filepath.Join(dir, "coordination")}, &out, &errOut); code != 0 {
		t.Fatal(code, &errOut)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("recovery without pending removed profile")
	}
}
