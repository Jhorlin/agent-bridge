package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxServiceInstallCLI(t *testing.T) {
	dir, profile := setup(t)
	binary := filepath.Join(dir, "binary")
	if err := os.WriteFile(binary, []byte("inert"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	run := func(context.Context, ...string) (string, error) {
		t.Fatal("install must not invoke systemctl")
		return "", nil
	}
	root := filepath.Join(dir, "config-home")
	if code := runLinuxServiceWith(context.Background(), []string{"install", profile}, root, binary, &out, &errOut, run); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	if code := runLinuxServiceWith(context.Background(), []string{"install", profile}, root, binary, &out, &errOut, run); code != 1 {
		t.Fatal(code)
	}
	for _, args := range [][]string{{}, {"unknown", profile}, {"start", profile, "--apply"}} {
		if code := runLinuxServiceWith(context.Background(), args, root, binary, &out, &errOut, run); code != 1 {
			t.Fatal(code)
		}
	}
}
