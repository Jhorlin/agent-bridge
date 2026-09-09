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

func TestHistoryCLI(t *testing.T) {
	dir, profile := setup(t)
	var out, errOut bytes.Buffer
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return Run(context.Background(), args, &out, &errOut)
	}
	if code := run("sync", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("history", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	var entries []bridge.HistoryEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	tx := entries[0].Transaction
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := run("sync", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("review-history", profile, tx, "rules", "codex", "after"); code != 0 {
		t.Fatal(code, &errOut)
	}
	var review bridge.ReviewCheckpoint
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"history"}, {"review-history", profile}, {"restore-reviewed", profile}, {"review-history", profile, tx, "rules", "invalid", "after"}} {
		if code := run(args...); code != 1 {
			t.Fatal(code, args)
		}
	}
	if code := run("restore-reviewed", profile, review.Observation, tx, "rules", "codex", "after"); code != 0 {
		t.Fatal(code, &errOut)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || string(data) != "one" {
		t.Fatal(string(data), err)
	}
	if code := Run(context.Background(), []string{"history", profile}, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
}
