package bridge

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDraftProfileReviewBoundary(t *testing.T) {
	for _, scope := range []string{"global", "project"} {
		f := newFixture(t)
		data, err := os.ReadFile("testdata/upstream/cli-creator/openai.yaml")
		must(t, err)
		// Actual upstream metadata in a synthetic, disposable skill wrapper.
		f.write(".agents/skills/demo/agents/openai.yaml", string(data))
		f.write(".agents/skills/demo/SKILL.md", "PRIVATE-CONTENT-SENTINEL")
		draft, err := DraftProfile(f.dir, scope, []string{"skill-directory-demo"})
		must(t, err)
		encoded, err := json.Marshal(draft)
		must(t, err)
		if !draft.ReadOnly || !draft.ReviewRequired || len(draft.Profile.Resources) != 1 || strings.Contains(string(encoded), "PRIVATE-CONTENT") || strings.Contains(string(encoded), "Create a composable CLI") {
			t.Fatal("unsafe draft")
		}
		r := draft.Profile.Resources[0]
		if r.Portable || r.AllowReformat || r.Scope != scope || len(r.Servers) > 0 {
			t.Fatal("implicit compatibility consent")
		}
		// Saving the whole envelope must never silently activate the selection.
		f.write("draft.json", string(encoded))
		if _, err := LoadConfig(f.path("draft.json")); err == nil {
			t.Fatal("draft became runnable")
		}
		profile, err := json.Marshal(draft.Profile)
		must(t, err)
		f.write("review.json", string(profile))
		if _, err := LoadConfig(f.path("review.json")); err == nil {
			t.Fatal("skill consent bypassed")
		}
		f.missing(".claude")
		f.missing(".agent-bridge")
		f.expect(".agents/skills/demo/agents/openai.yaml", string(data))
	}
}

func TestDraftProfileSelection(t *testing.T) {
	f := newFixture(t)
	for _, ids := range [][]string{nil, {"missing"}, {"instructions", "instructions"}, {"instructions", "INSTRUCTIONS"}, {"../instructions"}, {"INSTRUCTIONS"}} {
		if _, err := DraftProfile(f.dir, "project", ids); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	draft, err := DraftProfile(f.dir, "project", []string{"instructions"})
	must(t, err)
	if len(draft.Profile.Resources) != 1 || draft.Profile.Resources[0].ID != "instructions" {
		t.Fatal(draft)
	}
	if _, err := DraftProfile(f.dir, "invalid", []string{"instructions"}); err == nil {
		t.Fatal("invalid scope")
	}
	must(t, os.Symlink(f.path("CLAUDE.md"), f.path(".mcp.json")))
	if _, err := DraftProfile(f.dir, "project", []string{"instructions"}); err == nil {
		t.Fatal("unsafe inventory accepted")
	}
}
