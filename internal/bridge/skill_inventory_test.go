package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inventoryFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func inventoryFile(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

const inventorySkill = "---\nname: sample\ndescription: PRIVATE SKILL CONTENT\n---\nPRIVATE BODY\n"

func inventoryGroup(t *testing.T, r SkillInventory, name string) SkillMatch {
	t.Helper()
	for _, g := range r.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("missing group %s: %+v", name, r)
	return SkillMatch{}
}

func TestSkillInventorySharedAndCopies(t *testing.T) {
	for _, kind := range []string{"shared", "identical", "support-difference", "mode-difference", "release-difference"} {
		t.Run(kind, func(t *testing.T) {
			root := inventoryFixture(t)
			a := ".claude/skills/sample"
			b := ".agents/skills/sample"
			inventoryFile(t, root, b+"/SKILL.md", inventorySkill)
			if kind == "shared" {
				if err := os.MkdirAll(filepath.Join(root, ".claude/skills"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("../../.agents/skills/sample", filepath.Join(root, a)); err != nil {
					t.Skip(err)
				}
			} else {
				inventoryFile(t, root, a+"/SKILL.md", inventorySkill)
			}
			want := "identical-copies"
			switch kind {
			case "shared":
				want = "already-shared"
			case "support-difference":
				inventoryFile(t, root, a+"/scripts/check.sh", "echo different")
				want = "different-copies-review"
			case "mode-difference":
				if err := os.Chmod(filepath.Join(root, a, "SKILL.md"), 0700); err != nil {
					t.Fatal(err)
				}
				want = "different-copies-review"
			case "release-difference":
				inventoryFile(t, root, a+"/skill-release.json", `{"skillId":"sample","version":"1.0","channel":"stable"}`)
				inventoryFile(t, root, b+"/skill-release.json", `{"skillId":"sample","version":"2.0-dev","channel":"development"}`)
				want = "different-copies-review"
			}
			r, err := InventorySkills(root, false)
			if err != nil {
				t.Fatal(err)
			}
			if g := inventoryGroup(t, r, "sample"); g.Status != want {
				t.Fatalf("got %+v want %s", g, want)
			}
			out, _ := json.Marshal(r)
			if strings.Contains(string(out), "PRIVATE") {
				t.Fatal("leaked skill content")
			}
			if kind == "release-difference" {
				versions := map[string]bool{}
				for _, s := range r.Skills {
					versions[s.Version] = true
				}
				if !versions["1.0"] || !versions["2.0-dev"] {
					t.Fatal("lost release variants")
				}
			}
			before := string(out)
			again, err := InventorySkills(root, false)
			if err != nil {
				t.Fatal(err)
			}
			out, _ = json.Marshal(again)
			if string(out) != before {
				t.Fatal("nondeterministic inventory")
			}
		})
	}
}

func TestSkillInventoryCacheNotActivation(t *testing.T) {
	root := inventoryFixture(t)
	base := ".claude/plugins/cache/vendor/pkg/1.0"
	inventoryFile(t, root, base+"/skills/sample/SKILL.md", inventorySkill)
	inventoryFile(t, root, base+"/.codex-plugin/plugin.json", `{"name":"pkg","skills":"./skills"}`)
	inventoryFile(t, root, ".codex/skills/.system/sample/SKILL.md", inventorySkill)
	r, err := InventorySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skills) != 1 {
		t.Fatal("scanned cache without opt-in")
	}
	r, err = InventorySkills(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skills) != 2 {
		t.Fatalf("missing cache candidate %+v", r)
	}
	if inventoryGroup(t, r, "sample").Status != "cached-candidate-review" {
		t.Fatal("cache treated as active/equivalent")
	}
	found := false
	for _, s := range r.Skills {
		if s.Source == "plugin-cache" {
			found = s.NativeCodexManifest
		}
	}
	if !found {
		t.Fatal("missed upstream native variant")
	}
	// Additional cached versions must not be silently reduced to the newest.
	inventoryFile(t, root, ".claude/plugins/cache/vendor/pkg/2.0/skills/sample/SKILL.md", inventorySkill)
	r, err = InventorySkills(root, true)
	if err != nil || len(r.Skills) != 3 {
		t.Fatalf("lost cache versions: %v %+v", err, r)
	}
}

func TestSkillInventoryUnsafeAndMalformed(t *testing.T) {
	for _, kind := range []string{"escape", "chain", "nested", "bad-yaml", "missing", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			root := inventoryFixture(t)
			path := ".claude/skills/sample"
			inventoryFile(t, root, path+"/SKILL.md", inventorySkill)
			switch kind {
			case "escape", "chain":
				if err := os.Remove(filepath.Join(root, path, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				target := inventoryFixture(t)
				if kind == "chain" {
					if err := os.MkdirAll(filepath.Join(root, ".agents/skills"), 0700); err != nil {
						t.Fatal(err)
					}
					link := filepath.Join(root, ".agents/skills/other")
					if err := os.Symlink(target, link); err != nil {
						t.Skip(err)
					}
					target = link
				}
				if err := os.Symlink(target, filepath.Join(root, path)); err != nil {
					t.Skip(err)
				}
			case "nested":
				if err := os.Symlink("SKILL.md", filepath.Join(root, path, "extra")); err != nil {
					t.Skip(err)
				}
			case "bad-yaml":
				inventoryFile(t, root, path+"/SKILL.md", "---\nname: [\n---\n")
			case "missing":
				if err := os.Remove(filepath.Join(root, path, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(filepath.Join(root, path, "SKILL.md"), inventoryFileLimit+1); err != nil {
					t.Fatal(err)
				}
			}
			r, err := InventorySkills(root, false)
			if err != nil {
				t.Fatal(err)
			}
			if inventoryGroup(t, r, "sample").Status != "inspection-blocked" {
				t.Fatal("unsafe candidate accepted")
			}
		})
	}
}

func TestSkillInventoryBoundaries(t *testing.T) {
	root := inventoryFixture(t)
	// Broken links in account files and unrelated trees must never be touched.
	if err := os.Symlink("/nonexistent/private", filepath.Join(root, ".claude.json")); err != nil {
		t.Skip(err)
	}
	inventoryFile(t, root, "unregistered/.claude/skills/sample/SKILL.md", inventorySkill)
	r, err := InventorySkills(root, true)
	if err != nil || len(r.Skills) != 0 {
		t.Fatalf("unexpected scan: %v %+v", err, r)
	}
	if _, err := InventorySkills(filepath.Join(root, "missing"), false); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err := InventorySkills(filepath.VolumeName(root)+string(filepath.Separator), false); err == nil {
		t.Fatal("filesystem root accepted")
	}
	inventoryFile(t, root, ".claude/skills/sample/SKILL.md", inventorySkill)
	inventoryFile(t, root, ".codex/skills/sample/SKILL.md", inventorySkill)
	inventoryFile(t, root, ".codex/skills/.system/sample/SKILL.md", inventorySkill+"different")
	r, err = InventorySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skills) != 3 || inventoryGroup(t, r, "sample").Status != "different-copies-review" {
		t.Fatal("missed system collision")
	}
}

func TestSkillInventoryIdentityAndIgnoredDependencies(t *testing.T) {
	root := inventoryFixture(t)
	// Different directory names can expose the same declared skill name.
	inventoryFile(t, root, ".agents/skills/first/SKILL.md", inventorySkill)
	inventoryFile(t, root, ".codex/skills/second/SKILL.md", inventorySkill)
	r, err := InventorySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if inventoryGroup(t, r, "sample").Status != "same-host-collision" {
		t.Fatal("directory aliases hid collision")
	}
	inventoryFile(t, root, ".claude/skills/third/SKILL.md", inventorySkill)
	inventoryFile(t, root, ".claude/skills/third/node_modules/dependency/data", "PRIVATE DEPENDENCY")
	inventoryFile(t, root, ".claude/skills/third/.git/config", "PRIVATE GIT CONFIG")
	r, err = InventorySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if inventoryGroup(t, r, "sample").Status != "identical-copies" {
		t.Fatal("dependency exclusions inconsistent")
	}
	if _, err := InventorySkills(".", false); err == nil {
		t.Fatal("accepted implicit root")
	}
}

func TestSkillInventoryCacheLinksAndManifestEvidence(t *testing.T) {
	root := inventoryFixture(t)
	base := ".claude/plugins/cache/vendor/pkg/1"
	inventoryFile(t, root, base+"/skills/sample/SKILL.md", inventorySkill)
	inventoryFile(t, root, base+"/.codex-plugin/plugin.json", `{"name":123}`)
	r, err := InventorySkills(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Skills[0].NativeCodexManifest {
		t.Fatal("malformed manifest treated as native evidence")
	}
	if err := os.Symlink("1", filepath.Join(root, ".claude/plugins/cache/vendor/pkg/current")); err != nil {
		t.Skip(err)
	}
	if _, err := InventorySkills(root, true); err == nil {
		t.Fatal("followed cache link")
	}
}

func TestSkillInventoryIgnoresTemporaryPluginCheckouts(t *testing.T) {
	root := inventoryFixture(t)
	inventoryFile(t, root, ".claude/plugins/cache/temp_git_checkout/.git/config", "PRIVATE")
	if err := os.Symlink("outside", filepath.Join(root, ".claude/plugins/cache/temp_git_checkout/AGENTS.md")); err != nil {
		t.Skip(err)
	}
	inventoryFile(t, root, ".claude/plugins/cache/official/pkg/1/skills/sample/SKILL.md", inventorySkill)
	r, err := InventorySkills(root, true)
	if err != nil || len(r.Skills) != 1 {
		t.Fatalf("staging checkout blocked inventory: %v %+v", err, r)
	}
}

func TestSkillInventoryNestedPluginVariants(t *testing.T) {
	root := inventoryFixture(t)
	base := ".claude/plugins/cache/vendor/pkg/1/skills"
	inventoryFile(t, root, base+"/sample/SKILL.md", inventorySkill)
	inventoryFile(t, root, base+"/v1/sample/SKILL.md", inventorySkill+"legacy variant")
	r, err := InventorySkills(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skills) != 2 || len(r.Groups) != 1 || inventoryGroup(t, r, "sample").Status != "cached-candidate-review" {
		t.Fatalf("nested native variant missed: %+v", r)
	}
	for _, s := range r.Skills {
		if s.Status != "inspected" {
			t.Fatalf("nested variant blocked: %+v", s)
		}
	}
}
