package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlapCommand(t *testing.T) {
	dir, profile := setup(t)
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte(`{"version":1,"stateDir":"other-state","resources":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"check-overlap", profile, other}, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"readOnly":true`) {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	if code := Run(context.Background(), []string{"check-overlap", profile, other}, &auditFailWriter{}, &stderr); code != 1 {
		t.Fatal(code)
	}
	if code := Run(context.Background(), []string{"check-overlap", profile}, &out, &stderr); code != 1 {
		t.Fatal(code)
	}
	if err := os.WriteFile(other, []byte(`{"version":1,"stateDir":"state","resources":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"check-overlap", profile, other}, &out, &stderr); code != 2 {
		t.Fatal(code)
	}
	if err := os.WriteFile(other, []byte(`{"version":1,"stateDir":"other-state","resources":[],"PRIVATE_SENTINEL":"secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := Run(context.Background(), []string{"check-overlap", profile, other}, &out, &stderr); code != 1 || strings.Contains(stderr.String(), "PRIVATE_SENTINEL") || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("unsafe diagnostic: %d %s", code, &stderr)
	}
}
