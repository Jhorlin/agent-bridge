package bridge

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func pluginCandidateFixture(t *testing.T) (string, string) {
	t.Helper()
	root := inventoryFixture(t)
	inventoryFile(t, root, "left/.claude-plugin/plugin.json", `{"name":"same-name","version":"1","repository":"https://example.test/one","author":{"name":"one"}}`)
	inventoryFile(t, root, "right/.codex-plugin/plugin.json", `{"name":"same-name","version":"2","repository":"https://example.test/two","author":{"name":"two"},"apps":{"secret":"DO_NOT_REPORT"}}`)
	return filepath.Join(root, "left"), filepath.Join(root, "right")
}

// The read size itself is the resource contract: rejecting after ReadDir(-1)
// still permits unbounded allocation before the inventory budget is checked.
func TestPluginCandidatesDirectoryReadsStayWithinBudget(t *testing.T) {
	root := inventoryFixture(t)
	for index := 0; index < 400; index++ {
		inventoryFile(t, root, fmt.Sprintf("file-%04d", index), "unused")
	}
	for _, budget := range []int{0, 2, 399, 400} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			dir, err := os.Open(root)
			must(t, err)
			defer dir.Close()
			readCount := 0
			entries, err := candidateReadDir(func(n int) ([]fs.DirEntry, error) {
				if n <= 0 || n > 128 || n > budget-readCount+1 {
					t.Fatalf("unbounded directory read requested: n=%d remaining=%d", n, budget-readCount)
				}
				batch, err := dir.ReadDir(n)
				readCount += len(batch)
				return batch, err
			}, budget)
			if budget < 400 {
				if err == nil || len(entries) != 0 || readCount > budget+1 {
					t.Fatal("excessive directory was not rejected within budget")
				}
			} else if err != nil || len(entries) != 400 {
				t.Fatalf("exact-budget directory rejected: count=%d error=%v", len(entries), err)
			}
		})
	}
}

func TestPluginCandidatesRejectExcessivePublicInventory(t *testing.T) {
	left, right := pluginCandidateFixture(t)
	// Include the root, manifest directory and manifest in the 20,000-entry
	// package budget. The last file takes the total one over that budget.
	for index := 0; index < 19998; index++ {
		inventoryFile(t, left, fmt.Sprintf("file-%05d", index), "unopened")
	}
	_, err := ComparePluginCandidates(left, right)
	if err == nil || strings.Contains(err.Error(), left) {
		t.Fatal("excessive public inventory was not rejected privately")
	}
	must(t, os.Remove(filepath.Join(left, "file-19997")))
	if _, err := ComparePluginCandidates(left, right); err != nil {
		t.Fatal("exactly 20,000 inventory entries were rejected")
	}
}

func TestPluginCandidatesCompareEvidenceWithoutClaimingEquivalence(t *testing.T) {
	left, right := pluginCandidateFixture(t)
	inventoryFile(t, left, ".codex-plugin/plugin.json", `{"name":"same-name","version":"1","interface":{"displayName":"DO_NOT_REPORT"}}`)
	inventoryFile(t, right, "plugin.json", `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"same-name","version":"2"}`)
	for _, root := range []string{left, right} {
		inventoryFile(t, root, "skills/shared/SKILL.md", "INVALID YAML AND PRIVATE BODY")
		inventoryFile(t, root, ".env", "DO_NOT_REPORT")
		inventoryFile(t, root, "scripts/do-not-run.sh", "#!/bin/sh\nexit 99\n")
		must(t, os.Chmod(filepath.Join(root, "scripts/do-not-run.sh"), 0755))
	}
	inventoryFile(t, right, "skills/v1/extra/SKILL.md", "DO_NOT_REPORT")
	inventoryFile(t, left, "commands/one.md", "DO_NOT_REPORT")
	inventoryFile(t, right, "agents/one.md", "DO_NOT_REPORT")
	inventoryFile(t, left, "hooks/hooks.json", "INVALID JSON MUST NOT BE PARSED")
	inventoryFile(t, right, ".mcp.json", "INVALID JSON MUST NOT BE PARSED")
	before := auditTree(t, filepath.Dir(left))
	report, err := ComparePluginCandidates(left, right)
	must(t, err)
	if report.Status != "review-required" || !report.ReadOnly || len(report.Left.Manifests) != 2 || len(report.Right.Manifests) != 2 || len(report.Comparisons) != 4 {
		t.Fatalf("incomplete candidate report: %+v", report)
	}
	first := report.Comparisons[0]
	if first.Name != "match" || first.Version != "mismatch" || first.Repository != "mismatch" || first.Author != "mismatch" {
		t.Fatalf("same name hid provenance skew: %+v", first)
	}
	if got := report.Capabilities["skills"]; got != (PluginCapabilityComparison{Left: 1, Right: 2, CommonPaths: 1, RightOnly: 1}) {
		t.Fatalf("wrong skill inventory: %+v", got)
	}
	if report.Capabilities["commands"].LeftOnly != 1 || report.Capabilities["agents"].RightOnly != 1 || report.Capabilities["hookConfigs"].Left != 1 || report.Capabilities["mcpConfigs"].Right != 1 {
		t.Fatal("component paths not counted")
	}
	data, err := json.Marshal(report)
	must(t, err)
	for _, private := range []string{"DO_NOT_REPORT", "same-name", "https://example.test", left, right, "PRIVATE BODY", ".env"} {
		if strings.Contains(string(data), private) {
			t.Fatal("candidate report disclosed private input")
		}
	}
	if !reflect.DeepEqual(before, auditTree(t, filepath.Dir(left))) {
		t.Fatal("read-only candidate inspection wrote files")
	}
	// Same path names with different bodies are only structural overlap.
	inventoryFile(t, right, "skills/shared/SKILL.md", "CHANGED BODY")
	again, err := ComparePluginCandidates(left, right)
	must(t, err)
	if !reflect.DeepEqual(report, again) {
		t.Fatal("inspector depended on skill contents")
	}
}

func TestPluginCandidatesRejectUnsafeAndAmbiguousInputs(t *testing.T) {
	for _, kind := range []string{"relative-root", "missing-root", "filesystem-root", "linked-root", "linked-parent", "linked-manifest", "linked-body", "duplicate-key", "nested-duplicate", "malformed", "array", "missing-name", "invalid-name", "invalid-version", "invalid-author", "oversized", "manifest-alias", "manifest-directory", "deep-tree"} {
		t.Run(kind, func(t *testing.T) {
			left, right := pluginCandidateFixture(t)
			target := filepath.Join(left, ".claude-plugin/plugin.json")
			switch kind {
			case "relative-root":
				left = "PRIVATE_RELATIVE"
			case "missing-root":
				left = filepath.Join(left, "PRIVATE_MISSING")
			case "filesystem-root":
				left = string(filepath.Separator)
			case "linked-root":
				alias := left + "-alias"
				must(t, os.Symlink(left, alias))
				left = alias
			case "linked-parent":
				alias := filepath.Join(filepath.Dir(left), "alias")
				must(t, os.Symlink(filepath.Dir(left), alias))
				left = filepath.Join(alias, "left")
			case "linked-manifest":
				must(t, os.Rename(target, target+"-saved"))
				must(t, os.Symlink(target+"-saved", target))
			case "linked-body":
				must(t, os.Symlink(target, filepath.Join(left, "PRIVATE_LINK")))
			case "duplicate-key":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"first","name":"PRIVATE_SECRET"}`)
			case "nested-duplicate":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"first","apps":{"x":1,"x":"PRIVATE_SECRET"}}`)
			case "malformed":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"PRIVATE_SECRET"`)
			case "array":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `[]`)
			case "missing-name":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"version":"PRIVATE_SECRET"}`)
			case "invalid-name":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":["PRIVATE_SECRET"]}`)
			case "invalid-version":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"first","version":["PRIVATE_SECRET"]}`)
			case "invalid-author":
				inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"first","author":{"name":["PRIVATE_SECRET"]}}`)
			case "oversized":
				must(t, os.Truncate(target, 256*1024+1))
			case "manifest-alias":
				must(t, os.Rename(target, filepath.Join(left, ".claude-plugin/PLUGIN.JSON")))
			case "manifest-directory":
				must(t, os.Remove(target))
				must(t, os.Mkdir(target, 0700))
			case "deep-tree":
				inventoryFile(t, left, strings.Repeat("nested/", 34)+"file", "unused")
			}
			_, err := ComparePluginCandidates(left, right)
			if err == nil {
				t.Fatal("unsafe candidate accepted")
			}
			if strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), filepath.Dir(right)) {
				t.Fatal("candidate error leaked input")
			}
		})
	}
}

func TestPluginCandidatesMissingEvidenceAndManifestOnlyPackages(t *testing.T) {
	left, right := pluginCandidateFixture(t)
	inventoryFile(t, left, ".claude-plugin/plugin.json", `{"name":"one"}`)
	inventoryFile(t, right, ".codex-plugin/plugin.json", `{"name":"two","version":"1"}`)
	r, err := ComparePluginCandidates(left, right)
	must(t, err)
	if r.Comparisons[0].Name != "mismatch" || r.Comparisons[0].Version != "left-absent" || r.Comparisons[0].Repository != "both-absent" || r.Capabilities["skills"].Left != 0 {
		t.Fatal("missing evidence treated as matching provenance")
	}
}

func TestPluginCandidatesCountNestedCommandsWithoutReadingBodies(t *testing.T) {
	left, right := pluginCandidateFixture(t)
	for _, root := range []string{left, right} {
		inventoryFile(t, root, "commands/group/nested.md", "INVALID PRIVATE COMMAND BODY")
	}
	inventoryFile(t, right, "commands/another/deep/extra.md", "PRIVATE COMMAND BODY")
	inventoryFile(t, right, "commands/another/ignored.txt", "PRIVATE BODY")
	r, err := ComparePluginCandidates(left, right)
	must(t, err)
	if got := r.Capabilities["commands"]; got != (PluginCapabilityComparison{Left: 1, Right: 2, CommonPaths: 1, RightOnly: 1}) {
		t.Fatalf("nested command paths were not counted: %+v", got)
	}
	inventoryFile(t, right, "commands/group/nested.md", "DIFFERENT PRIVATE BODY")
	again, err := ComparePluginCandidates(left, right)
	must(t, err)
	if !reflect.DeepEqual(r, again) {
		t.Fatal("candidate command counts depended on bodies")
	}
}
