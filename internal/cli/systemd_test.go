package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdExportCommand(t *testing.T) {
	_, profile := setup(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// macOS exposes Go's temporary executable through /var, a symlink to
	// /private/var. Pass the canonical fixture path; production rejects links.
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	args := []string{"systemd-unit", profile, binary}
	if code := Run(context.Background(), args, &out, &errOut); code != 0 || strings.Contains(out.String(), " --apply") {
		t.Fatalf("%d %s", code, &errOut)
	}
	if code := Run(context.Background(), args, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
	for _, bad := range [][]string{{"systemd-unit"}, {"systemd-unit", profile, binary, "--start"}, {"systemd-unit", profile, "missing"}} {
		if code := Run(context.Background(), bad, &out, &errOut); code != 1 {
			t.Fatal(code)
		}
	}
}
