package bridge

// Skill inventory is deliberately separate from convention enrollment. A cache
// hit is evidence to review, never authorization to overwrite or install a skill.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type SkillInventory struct {
	Version  int                 `json:"version"`
	ReadOnly bool                `json:"readOnly"`
	Root     string              `json:"root"`
	Skills   []SkillInstallation `json:"skills"`
	Groups   []SkillMatch        `json:"groups"`
	Warnings []string            `json:"warnings"`
}

type SkillInstallation struct {
	Host                string `json:"host"`
	Source              string `json:"source"`
	Path                string `json:"path"`
	PhysicalPath        string `json:"physicalPath,omitempty"`
	Name                string `json:"name"`
	Version             string `json:"version,omitempty"`
	Channel             string `json:"channel,omitempty"`
	Digest              string `json:"digest,omitempty"`
	NativeCodexManifest bool   `json:"nativeCodexManifest"`
	Status              string `json:"status"`
}

type SkillMatch struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Paths  []string `json:"paths"`
}

const inventoryFileLimit = 8 << 20
const inventoryTreeLimit = 64 << 20
const inventoryEntryLimit = 20000

// InventorySkills inspects only conventional skill roots and, when requested,
// the two plugin caches. It does not read account/config/credential stores.
// Versions and matching names are hints; only a full inspected tree digest can
// establish byte equality, and none of these results establishes portability.
func InventorySkills(root string, plugins bool) (SkillInventory, error) {
	r := SkillInventory{Version: 1, ReadOnly: true, Skills: []SkillInstallation{}, Groups: []SkillMatch{}, Warnings: []string{
		"No files changed or enrolled. Matching names do not prove equivalence or portability.",
		"Plugin cache presence does not prove installation or enablement; native manifests do not prove runtime compatibility.",
		"Digests exclude .git and node_modules; all other regular supporting files and executable bits are included. No scripts are executed.",
		"Custom configuration roots, marketplace catalogs, project trees, credentials and host settings are not inspected.",
		"Hidden and temp_ staging directories at the plugin-cache root are excluded.",
	}}
	if !filepath.IsAbs(root) {
		return r, fmt.Errorf("inventory requires an absolute root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return r, err
	}
	r.Root = abs
	if abs == filepath.Dir(abs) {
		return r, fmt.Errorf("filesystem root is not an inventory root")
	}
	if err := assertSafe(abs); err != nil {
		return r, err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return r, fmt.Errorf("invalid inventory root")
	}
	personal := []struct{ path, host, source string }{
		{".claude/skills", "claude", "personal"}, {".agents/skills", "codex", "personal"},
		{".codex/skills", "codex", "legacy"}, {".codex/skills/.system", "codex", "system"},
	}
	count := 0
	add := func(path, host, source string, native bool) error {
		count++
		if count > inventoryEntryLimit {
			return fmt.Errorf("inventory limit exceeded")
		}
		s := inspectSkillInstallation(abs, path, host, source, native)
		r.Skills = append(r.Skills, s)
		return nil
	}
	var pluginSkill func(string, string, bool, int) error
	pluginSkill = func(path, host string, native bool, depth int) error {
		count++
		if count > inventoryEntryLimit {
			return fmt.Errorf("inventory limit exceeded")
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return add(path, host, "plugin-cache", native)
		}
		_, err = os.Lstat(filepath.Join(path, "SKILL.md"))
		if !os.IsNotExist(err) || depth >= 4 {
			return add(path, host, "plugin-cache", native)
		}
		entries, err := inventoryDirs(path)
		if err != nil {
			return err
		}
		children := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
				continue
			}
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
				children++
				if err := pluginSkill(filepath.Join(path, e.Name()), host, native, depth+1); err != nil {
					return err
				}
			}
		}
		if children == 0 {
			return add(path, host, "plugin-cache", native)
		}
		return nil
	}
	for _, dir := range personal {
		base := filepath.Join(abs, dir.path)
		entries, err := inventoryDirs(base)
		if err != nil {
			return r, err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.IsDir() || e.Type()&os.ModeSymlink != 0 {
				if err := add(filepath.Join(base, e.Name()), dir.host, dir.source, false); err != nil {
					return r, err
				}
			}
		}
	}
	if plugins {
		for _, host := range []string{"claude", "codex"} {
			base := filepath.Join(abs, "."+host, "plugins", "cache")
			// Cache layout: marketplace/package/version/skills/name. Never walk
			// arbitrary package trees, registry paths, or symlinked packages.
			var walk func(string, int) error
			walk = func(dir string, depth int) error {
				entries, err := inventoryDirs(dir)
				if err != nil {
					return err
				}
				for _, e := range entries {
					if depth == 0 && (strings.HasPrefix(e.Name(), "temp_") || strings.HasPrefix(e.Name(), ".")) {
						continue
					}
					count++
					if count > inventoryEntryLimit {
						return fmt.Errorf("inventory limit exceeded")
					}
					if e.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("unsafe cache link")
					}
					if !e.IsDir() {
						continue
					}
					p := filepath.Join(dir, e.Name())
					if depth < 2 {
						if err := walk(p, depth+1); err != nil {
							return err
						}
						continue
					}
					native := false
					if data, err := inventoryRead(filepath.Join(p, ".codex-plugin", "plugin.json")); err == nil {
						var m struct {
							Name string `json:"name"`
						}
						native = json.Unmarshal(data, &m) == nil && safeID.MatchString(m.Name)
					}
					skills, err := inventoryDirs(filepath.Join(p, "skills"))
					if err != nil {
						return err
					}
					for _, skill := range skills {
						if skill.IsDir() || skill.Type()&os.ModeSymlink != 0 {
							if err := pluginSkill(filepath.Join(p, "skills", skill.Name()), host, native, 0); err != nil {
								return err
							}
						}
					}
				}
				return nil
			}
			if err := walk(base, 0); err != nil {
				return r, err
			}
		}
	}
	sort.Slice(r.Skills, func(i, j int) bool { return r.Skills[i].Path < r.Skills[j].Path })
	byName := map[string][]SkillInstallation{}
	for _, s := range r.Skills {
		byName[s.Name] = append(byName[s.Name], s)
	}
	names := []string{}
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		items := byName[name]
		g := SkillMatch{Name: name, Status: "single-host-review", Paths: []string{}}
		hosts := map[string]bool{}
		physical := map[string]bool{}
		digests := map[string]bool{}
		cached, invalid := false, false
		for _, s := range items {
			g.Paths = append(g.Paths, s.Path)
			hosts[s.Host] = true
			physical[s.PhysicalPath] = true
			digests[s.Digest] = true
			cached = cached || s.Source == "plugin-cache"
			invalid = invalid || s.Status != "inspected"
		}
		switch {
		case invalid:
			g.Status = "inspection-blocked"
		case cached:
			g.Status = "cached-candidate-review"
		case len(hosts) > 1 && len(physical) == 1:
			g.Status = "already-shared"
		case len(hosts) > 1 && len(digests) == 1:
			g.Status = "identical-copies"
		case len(hosts) > 1:
			g.Status = "different-copies-review"
		case len(items) > 1:
			g.Status = "same-host-collision"
		}
		r.Groups = append(r.Groups, g)
	}
	return r, nil
}

func inventoryDirs(path string) ([]os.DirEntry, error) {
	if err := assertSafe(path); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if len(entries) > inventoryEntryLimit {
		return nil, fmt.Errorf("inventory limit exceeded")
	}
	return entries, err
}

func inventoryRead(path string) ([]byte, error) {
	if err := assertSafe(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > inventoryFileLimit {
		return nil, fmt.Errorf("unsupported inventory file")
	}
	f, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("inventory file changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, inventoryFileLimit+1))
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || len(data) > inventoryFileLimit || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		return nil, fmt.Errorf("inventory file changed")
	}
	return data, nil
}

func inspectSkillInstallation(root, path, host, source string, native bool) SkillInstallation {
	s := SkillInstallation{Host: host, Source: source, Path: path, Name: filepath.Base(path), NativeCodexManifest: native, Status: "inspection-blocked"}
	physical := path
	if err := assertSafe(filepath.Dir(path)); err != nil {
		return s
	}
	st, err := os.Lstat(path)
	if err != nil {
		return s
	}
	if st.Mode()&os.ModeSymlink != 0 {
		// Only a direct personal skill link to another personal skill root is
		// inspectable. Cache links, chained links and outside-home links block.
		if source != "personal" && source != "legacy" {
			return s
		}
		target, err := os.Readlink(path)
		if err != nil {
			return s
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		physical = filepath.Clean(target)
		allowed := false
		for _, base := range []string{".claude/skills", ".agents/skills", ".codex/skills"} {
			if filepath.Dir(physical) == filepath.Join(root, base) {
				allowed = true
			}
		}
		if !allowed {
			return s
		}
	}
	if err := assertSafe(physical); err != nil {
		return s
	}
	entry, err := inventoryRead(filepath.Join(physical, "SKILL.md"))
	if err != nil {
		return s
	}
	// Read frontmatter only as data. Never output descriptions or skill bodies.
	lines := strings.Split(strings.ReplaceAll(string(entry), "\r\n", "\n"), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return s
	}
	end := 0
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == 0 {
		return s
	}
	var meta struct {
		Name     string `yaml:"name"`
		Metadata struct {
			Version string `yaml:"version"`
		} `yaml:"metadata"`
	}
	if yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &meta) != nil || !safeID.MatchString(meta.Name) {
		return s
	}
	s.Name = meta.Name
	if len(meta.Metadata.Version) <= 128 {
		s.Version = meta.Metadata.Version
	}
	if data, err := inventoryRead(filepath.Join(physical, "skill-release.json")); err == nil {
		var release struct {
			SkillID string `json:"skillId"`
			Version string `json:"version"`
			Channel string `json:"channel"`
		}
		if json.Unmarshal(data, &release) == nil && release.SkillID == s.Name && len(release.Version) <= 128 && len(release.Channel) <= 128 {
			s.Version = release.Version
			s.Channel = release.Channel
		}
	}
	digest, err := inventoryDigest(physical)
	if err != nil {
		return s
	}
	s.PhysicalPath = physical
	s.Digest = digest
	s.Status = "inspected"
	return s
}

func inventoryDigest(root string) (string, error) {
	h := sha256.New()
	total, count := 0, 0
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		count++
		if count > inventoryEntryLimit {
			return fmt.Errorf("inventory limit exceeded")
		}
		if e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested skill link")
		}
		if e.IsDir() {
			if path != root && (e.Name() == ".git" || e.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := inventoryRead(path)
		if err != nil {
			return err
		}
		total += len(data)
		if total > inventoryTreeLimit {
			return fmt.Errorf("inventory size exceeded")
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		// Length-delimited JSON avoids concatenation ambiguity between paths.
		record, _ := json.Marshal([]any{filepath.ToSlash(rel), info.Mode().Perm() & 0111, hex.EncodeToString(sum[:])})
		io.Copy(h, bytes.NewReader(record))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
