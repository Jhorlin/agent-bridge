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

func TestReviewCommands(t *testing.T) {
	dir, file := setup(t)
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"review-profile", file}, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	var r bridge.ReviewCheckpoint
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"review-profile", file}, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"sync-reviewed", file, r.Observation}, &out, &errOut); code != 2 {
		t.Fatal(code)
	}
	fresh, err := bridge.ReviewProfile(file)
	if err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"sync-reviewed", file, fresh.Observation}, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	for _, args := range [][]string{{"review-profile"}, {"sync-reviewed", file}, {"sync-reviewed", file, ""}, {"review-profile", filepath.Join(dir, "missing")}} {
		if code := Run(context.Background(), args, &out, &errOut); code != 1 {
			t.Fatal(code)
		}
	}
}
