package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDraftProfileCommand(t *testing.T) {
	dir, _ := setup(t)
	var out, stderr bytes.Buffer
	args := []string{"draft-profile", dir, "--project", "instructions"}
	if code := Run(context.Background(), args, &out, &stderr); code != 0 || !strings.Contains(out.String(), `"reviewRequired":true`) {
		t.Fatalf("%d %s %s", code, &out, &stderr)
	}
	if code := Run(context.Background(), args, &auditFailWriter{}, &stderr); code != 1 {
		t.Fatal(code)
	}
	for _, bad := range [][]string{{"draft-profile"}, {"draft-profile", dir, "--apply", "instructions"}, {"draft-profile", dir, "--project", "MISSING_PRIVATE_ID"}} {
		stderr.Reset()
		if code := Run(context.Background(), bad, &out, &stderr); code != 1 || strings.Contains(stderr.String(), "MISSING_PRIVATE_ID") {
			t.Fatalf("%d %s", code, &stderr)
		}
	}
}
