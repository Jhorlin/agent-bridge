package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func TestRetirementCLI(t *testing.T) {
	_, profile := setup(t)
	var out, errOut bytes.Buffer
	run := func(args ...string) int {
		out.Reset()
		errOut.Reset()
		return Run(context.Background(), args, &out, &errOut)
	}
	c, err := bridge.LoadConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	id := c.Resources[0].ID
	if code := run("review-retirement", profile, id); code != 0 {
		t.Fatal(code, &errOut)
	}
	var review bridge.Retirement
	if err := json.Unmarshal(out.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if code := run("apply-retirement", profile, "bad", id); code != 2 {
		t.Fatal(code, &errOut)
	}
	if code := run("apply-retirement", profile, review.Observation, id); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("sync", profile); code != 0 {
		t.Fatal(code, &errOut)
	}
	if code := run("review-retirement", profile); code == 0 {
		t.Fatal("accepted missing ID")
	}
}
