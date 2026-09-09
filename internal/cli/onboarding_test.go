package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingCommands(t *testing.T) {
	dir, _ := setup(t)
	profile := filepath.Join(dir, "new.json")
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"init", profile}, &out, &errs); code != 0 {
		t.Fatalf("init: %d %s", code, errs.String())
	}
	if code := Run(context.Background(), []string{"init", profile}, &out, &errs); code != 1 {
		t.Fatal("overwrote profile")
	}
	out.Reset()
	if code := Run(context.Background(), []string{"discover", dir, "--project"}, &out, &errs); code != 0 || !strings.Contains(out.String(), `"readOnly":true`) {
		t.Fatalf("discover: %d %s", code, out.String())
	}
	for _, args := range [][]string{{"init", profile, "--apply"}, {"discover", dir}, {"discover", dir, "--apply"}} {
		if code := Run(context.Background(), args, &out, &errs); code != 1 {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	canceled := filepath.Join(dir, "canceled.json")
	Run(ctx, []string{"init", canceled}, &out, &errs)
	if _, err := os.Stat(canceled); !os.IsNotExist(err) {
		t.Fatal("canceled init wrote a file")
	}
}
