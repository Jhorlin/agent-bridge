package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func TestEnrollReviewedCLI(t *testing.T) {
	dir, profile := setup(t)
	data := []byte(`{"version":1,"stateDir":"state","coordinationDir":"coordination","resources":[]}`)
	if err := os.WriteFile(profile, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := bridge.ReviewProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"enroll-reviewed", profile, r.Observation}, &out, &errOut); code != 0 {
		t.Fatalf("%d %s", code, &errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatal("enrollment created sync state")
	}
	for _, args := range [][]string{{"enroll-reviewed"}, {"enroll-reviewed", profile, ""}} {
		if code := Run(context.Background(), args, &out, &errOut); code != 1 {
			t.Fatal(code)
		}
	}
	if code := Run(context.Background(), []string{"enroll-reviewed", profile, r.Observation}, &auditFailWriter{}, &errOut); code != 1 {
		t.Fatal(code)
	}
}
