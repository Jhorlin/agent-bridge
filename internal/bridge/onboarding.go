package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Discovery struct {
	Version    int             `json:"version"`
	ReadOnly   bool            `json:"readOnly"`
	Root       string          `json:"root"`
	Scope      string          `json:"scope"`
	Candidates []resourceInput `json:"candidates"`
	Review     []string        `json:"review"`
}

// Discover inventories explicit conventional roots only. It reads directory
// entries and file metadata, never native file contents or credential stores.
// Returned resources are deliberately NOT enrolled or marked portable.
func Discover(root, scope string) (Discovery, error) {
	report := Discovery{Version: 1, ReadOnly: true, Scope: scope, Candidates: []resourceInput{}, Review: []string{
		"Candidates are not enrolled. Review content and set portable/allowReformat only where required and justified.",
		"Run init for an empty profile, add reviewed candidates, select MCP server names explicitly, then audit and plan.",
		"Global discovery uses conventional paths under the explicit root; custom host configuration roots require manual paths.",
		"Installed plugin caches, credentials, histories, automatic memories and unregistered project trees are not scanned.",
	}}
	if scope != "global" && scope != "project" {
		return report, fmt.Errorf("scope must be global or project")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return report, err
	}
	report.Root = absolute
	if err := assertSafe(absolute); err != nil {
		return report, err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return report, fmt.Errorf("discovery root must be an existing safe directory")
	}
	claude := filepath.Join(absolute, ".claude")
	codex := filepath.Join(absolute, ".codex")
	claudeInstructions := filepath.Join(absolute, "CLAUDE.md")
	codexInstructions := filepath.Join(absolute, "AGENTS.md")
	if scope == "global" {
		claudeInstructions = filepath.Join(claude, "CLAUDE.md")
		codexInstructions = filepath.Join(codex, "AGENTS.md")
	}
	add := func(id, kind, a, b string) error {
		exists := false
		for _, path := range []string{a, b} {
			if err := assertSafe(path); err != nil {
				return err
			}
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() && !(kind == "skill-directory" && info.IsDir()) {
				return fmt.Errorf("unexpected candidate file type")
			}
			exists = true
		}
		if exists {
			report.Candidates = append(report.Candidates, resourceInput{ID: id, Kind: kind, Scope: scope, Claude: a, Codex: b})
		}
		return nil
	}
	// A plain file is offered rather than inserting instruction markers without
	// consent. Users may choose instruction-file after preparing shared sections.
	if err := add("instructions", "portable-file", claudeInstructions, codexInstructions); err != nil {
		return report, err
	}
	if err := add("startup-hooks", "hook-config", filepath.Join(claude, "settings.json"), filepath.Join(codex, "hooks.json")); err != nil {
		return report, err
	}
	if scope == "project" {
		if err := add("mcp", "mcp-config", filepath.Join(absolute, ".mcp.json"), filepath.Join(codex, "config.toml")); err != nil {
			return report, err
		}
	}
	pairs := []struct{ kind, a, b, suffixA, suffixB string }{
		{"skill-directory", filepath.Join(claude, "skills"), filepath.Join(absolute, ".agents", "skills"), "", ""},
		{"agent-file", filepath.Join(claude, "agents"), filepath.Join(codex, "agents"), ".md", ".toml"},
	}
	for _, pair := range pairs {
		names := map[string]bool{}
		for i, dir := range []string{pair.a, pair.b} {
			if err := assertSafe(dir); err != nil {
				return report, err
			}
			entries, err := os.ReadDir(dir)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return report, err
			}
			suffix := pair.suffixA
			if i == 1 {
				suffix = pair.suffixB
			}
			for _, entry := range entries {
				if entry.Type()&os.ModeSymlink != 0 {
					return report, fmt.Errorf("discovery does not follow symlink candidates")
				}
				name := entry.Name()
				if pair.kind == "skill-directory" {
					if !entry.IsDir() {
						continue
					}
				} else {
					if !strings.HasSuffix(name, suffix) {
						continue
					}
					name = strings.TrimSuffix(name, suffix)
				}
				if !safeID.MatchString(name) {
					continue
				}
				names[name] = true
			}
		}
		ordered := []string{}
		for name := range names {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		for _, name := range ordered {
			if err := add(pair.kind+"-"+name, pair.kind, filepath.Join(pair.a, name+pair.suffixA), filepath.Join(pair.b, name+pair.suffixB)); err != nil {
				return report, err
			}
		}
	}
	sort.Slice(report.Candidates, func(i, j int) bool { return report.Candidates[i].ID < report.Candidates[j].ID })
	return report, nil
}

// InitProfile creates only a private, empty profile; never overwrite an existing
// file, create host config, or enroll resources based on filename similarity.
func InitProfile(filename string) error {
	return initProfile(filename, nil)
}

// InitConventionProfile creates a profile that selects the containing project,
// not individual files. Creating the profile itself performs no synchronization.
func InitConventionProfile(filename string) error {
	return initProfile(filename, &Conventions{Root: ".", Scope: "project", Features: append([]string{}, conventionFeatures...)})
}

func InitGlobalConventionProfile(filename, root string) error {
	if root == "" || strings.HasPrefix(root, "~") {
		return fmt.Errorf("global conventions require an explicit root path")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := assertSafe(absolute); err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("global convention root must exist")
	}
	policy := &Conventions{Root: absolute, Scope: "global", Features: append([]string{}, conventionFeatures...)}
	if err := policy.resolve(filepath.Dir(absolute)); err != nil {
		return err
	}
	return initProfile(filename, policy)
}

func initProfile(filename string, conventions *Conventions) error {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	if err := assertSafe(absolute); err != nil {
		return err
	}
	raw := configInput{Version: 1, StateDir: ".agent-bridge", Resources: []resourceInput{}}
	raw.Conventions = conventions
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}
