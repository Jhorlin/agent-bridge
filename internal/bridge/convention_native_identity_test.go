package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Collision checks need fresh names, not the bytes of supporting assets. Full
// inventory and sync retain their independent content/safety validation.
func TestNativeSkillGuardInspectsIdentityWithoutSupportingContents(t *testing.T) {
	f, _, _ := nativeGuardRenameFixture(t)
	plan := f.plan()
	f.write(".claude/skills/alias/large-asset", "")
	must(t, os.Truncate(f.path(".claude/skills/alias/large-asset"), inventoryFileLimit+1))
	_, err := LoadConfig(f.path("config.json"))
	must(t, err)
	if err := validatePlannedNativeSkills(f.c, plan); err != nil {
		t.Fatalf("identity guard inspected supporting contents: %v", err)
	}
	full, err := InventorySkills(f.dir, true)
	must(t, err)
	if inventoryGroup(t, full, "new-name").Status != "inspection-blocked" {
		t.Fatal("full inventory no longer validates supporting assets")
	}
	// An unrelated cache identity must not block; a later rename must block
	// immediately, even with unreadable/oversized supporting content.
	f.write(".codex/plugins/cache/vendor/pkg/1/skills/native/SKILL.md", "---\nname: unrelated\ndescription: Native.\n---\nNative.\n")
	must(t, validatePlannedNativeSkills(f.c, plan))
	f.write(".codex/plugins/cache/vendor/pkg/1/skills/native/SKILL.md", "---\nname: new-name\ndescription: Native.\n---\nNative.\n")
	if err := validatePlannedNativeSkills(f.c, plan); err == nil {
		t.Fatal("identity guard cached a stale native name")
	}
}

func TestSkillIdentityInspectionStillRejectsUnsafeMetadata(t *testing.T) {
	for _, kind := range []string{"malformed", "oversized", "entry-link", "cache-link"} {
		t.Run(kind, func(t *testing.T) {
			root := inventoryFixture(t)
			path := ".codex/plugins/cache/vendor/pkg/1/skills/sample"
			inventoryFile(t, root, path+"/SKILL.md", inventorySkill)
			switch kind {
			case "malformed":
				inventoryFile(t, root, path+"/SKILL.md", "---\nname: [\n---\n")
			case "oversized":
				must(t, os.Truncate(filepath.Join(root, path, "SKILL.md"), inventoryFileLimit+1))
			case "entry-link":
				must(t, os.Rename(filepath.Join(root, path, "SKILL.md"), filepath.Join(root, path, "target")))
				must(t, os.Symlink("target", filepath.Join(root, path, "SKILL.md")))
			case "cache-link":
				must(t, os.Rename(filepath.Join(root, path), filepath.Join(root, "target")))
				must(t, os.Symlink(filepath.Join(root, "target"), filepath.Join(root, path)))
			}
			identities, err := inventorySkillIdentities(root)
			must(t, err)
			if len(identities) != 1 || identities[0].Status != "inspection-blocked" || identities[0].Digest != "" {
				t.Fatalf("unsafe identity accepted: %+v", identities)
			}
		})
	}
}

func BenchmarkSkillInventoryGuard(b *testing.B) {
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		path := filepath.Join(root, ".codex/plugins/cache/vendor/pkg/1/skills", fmt.Sprintf("skill-%d", i))
		if err := os.MkdirAll(path, 0700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(inventorySkill), 0600); err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 8; j++ {
			if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("asset-%d", j)), make([]byte, 64<<10), 0600); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := InventorySkills(root, true); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("identity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := inventorySkillIdentities(root); err != nil {
				b.Fatal(err)
			}
		}
	})
}
