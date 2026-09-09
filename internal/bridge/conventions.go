package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// Conventions opts an explicit project tree or global root into feature discovery.
// Exclude entries are root-relative files or directory subtrees, not globs.
// Resource identities and all writes still use the ordinary transaction engine.
type Conventions struct {
	Root     string   `json:"root"`
	Exclude  []string `json:"exclude,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Features []string `json:"features,omitempty"`
}

// Unknown policy keys must fail even in the legacy permissive config loader:
// misspelling an exclusion must never broaden an automatically writable tree.
func (c *Conventions) UnmarshalJSON(data []byte) error {
	type plain Conventions
	var raw plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("invalid convention policy")
	}
	*c = Conventions(raw)
	return nil
}

const conventionPrefix = "auto-instructions-"

func (c *Conventions) resolve(base string) error {
	if c.Scope != "" && c.Scope != "project" && c.Scope != "global" {
		return fmt.Errorf("conventions.scope must be project or global")
	}
	seen := map[string]bool{}
	if c.Features != nil && len(c.Features) == 0 {
		return fmt.Errorf("conventions.features must select at least one feature")
	}
	for _, feature := range c.Features {
		if !hasField(conventionFeatures, feature) || seen[feature] {
			return fmt.Errorf("invalid or duplicate convention feature")
		}
		seen[feature] = true
	}
	if c.Root == "" || strings.HasPrefix(c.Root, "~") {
		return fmt.Errorf("conventions.root requires an explicit project path")
	}
	c.Root = resolve(base, c.Root)
	if filepath.Dir(c.Root) == c.Root {
		return fmt.Errorf("conventions.root must not be a filesystem root")
	}
	for _, path := range c.Exclude {
		if path == "" || path == "." || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\\*?[") || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return fmt.Errorf("conventions.exclude requires clean project-relative file or directory paths, not globs")
		}
	}
	return nil
}

func (c *Conventions) excluded(path string) bool {
	for _, entry := range c.Exclude {
		if inside(strings.ToLower(filepath.Join(c.Root, entry)), strings.ToLower(path)) {
			return true
		}
	}
	return false
}

func conventionEntry(root, dir string) resourceInput {
	rel, _ := filepath.Rel(root, dir)
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	return resourceInput{ID: fmt.Sprintf("%s%x", conventionPrefix, sum), Kind: "portable-file", Scope: "project", Claude: filepath.Join(dir, "CLAUDE.md"), Codex: filepath.Join(dir, "AGENTS.md")}
}

var conventionSkippedDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, "target": true, "__pycache__": true, "venv": true,
}

func conventionStorage(c Config, path string) bool {
	path = strings.ToLower(path)
	return inside(strings.ToLower(c.StateDir), path) || (c.CoordinationDir != "" && inside(strings.ToLower(c.CoordinationDir), path))
}

func discoverInstructionConventions(c Config, explicit []resourceInput) ([]resourceInput, []string, error) {
	policy := c.Conventions
	if err := assertSafe(policy.Root); err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(policy.Root)
	if err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("conventions.root must be an existing safe directory")
	}
	for _, path := range []string{c.StateDir, c.CoordinationDir} {
		if path != "" && inside(strings.ToLower(path), strings.ToLower(policy.Root)) {
			return nil, nil, fmt.Errorf("convention root overlaps state or coordination directory")
		}
	}
	entries := map[string]resourceInput{}
	warnings := map[string]bool{}
	for _, r := range explicit {
		if strings.HasPrefix(r.ID, conventionPrefix) {
			return nil, nil, fmt.Errorf("auto-instructions- IDs are reserved for convention discovery")
		}
	}
	managed := func(path string) bool {
		for _, r := range explicit {
			if r.Claude == path || r.Codex == path {
				return true
			}
		}
		return false
	}
	// Explicit resources take precedence only when they own the exact pair.
	add := func(dir string) {
		r := conventionEntry(policy.Root, dir)
		if policy.excluded(r.Claude) || policy.excluded(r.Codex) {
			return
		}
		for _, old := range explicit {
			if old.Claude == r.Claude && old.Codex == r.Codex {
				return
			}
		}
		entries[r.ID] = r
	}
	skipped := []string{}
	err = filepath.WalkDir(policy.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if policy.excluded(path) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != policy.Root {
				name := entry.Name()
				if strings.HasPrefix(name, ".") || conventionSkippedDirs[strings.ToLower(name)] || conventionStorage(c, path) {
					skipped = append(skipped, path)
					return filepath.SkipDir
				}
				if _, e := os.Lstat(filepath.Join(path, ".git")); e == nil {
					skipped = append(skipped, path)
					return filepath.SkipDir
				} else if !os.IsNotExist(e) {
					return e
				}
			}
			alternate := filepath.Join(path, ".claude", "CLAUDE.md")
			if !policy.excluded(alternate) && !managed(alternate) {
				if err := assertSafe(filepath.Dir(alternate)); err != nil {
					return err
				}
				if _, e := os.Lstat(alternate); e == nil {
					return fmt.Errorf("conventions: .claude/CLAUDE.md requires explicit mapping or exclusion")
				} else if !os.IsNotExist(e) {
					return e
				}
			}
			return nil
		}
		name := entry.Name()
		if name == "CLAUDE.local.md" {
			warnings["Private CLAUDE.local.md files are excluded; their instructions are not synchronized."] = true
			return nil
		}
		if name == "AGENTS.override.md" && !managed(path) {
			return fmt.Errorf("conventions: AGENTS.override.md requires explicit mapping or exclusion; plain AGENTS.md may be shadowed")
		}
		if strings.EqualFold(name, "CLAUDE.md") || strings.EqualFold(name, "AGENTS.md") {
			if name != "CLAUDE.md" && name != "AGENTS.md" {
				return fmt.Errorf("conventions: instruction filename case collision")
			}
			if err := assertSafe(path); err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := checkRegular(info); err != nil {
				return err
			}
			add(filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Retain pairs even when both native files disappear. Reconstruct identities
	// rather than trusting writable targets from the manifest.
	tracked, err := conventionTracked(c)
	if err != nil {
		return nil, nil, err
	}
	for id, previous := range tracked {
		if !strings.HasPrefix(id, conventionPrefix) {
			continue
		}
		dir := filepath.Dir(previous.Paths["claude"])
		if !inside(policy.Root, dir) {
			return nil, nil, fmt.Errorf("conventions: tracked root changed; preserve the old profile for recovery")
		}
		r := conventionEntry(policy.Root, dir)
		expected := Resource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Paths: map[string]string{"claude": r.Claude, "codex": r.Codex, "shared": filepath.Join(c.StateDir, "shared", r.ID)}}
		if id != r.ID || !reflect.DeepEqual(expected, previous) {
			return nil, nil, fmt.Errorf("conventions: invalid tracked instruction identity")
		}
		if policy.excluded(r.Claude) || policy.excluded(r.Codex) {
			continue
		}
		// Check absent ancestors too: a manifest must not resurrect files under
		// a dependency, hidden directory, or private state root.
		for ancestor := dir; ancestor != policy.Root; ancestor = filepath.Dir(ancestor) {
			name := filepath.Base(ancestor)
			if strings.HasPrefix(name, ".") || conventionSkippedDirs[strings.ToLower(name)] || conventionStorage(c, ancestor) {
				return nil, nil, fmt.Errorf("conventions: tracked instruction crosses an excluded boundary")
			}
		}
		for _, boundary := range skipped {
			if inside(boundary, dir) {
				return nil, nil, fmt.Errorf("conventions: a tracked pair moved behind an excluded boundary; explicitly exclude or restore it")
			}
		}
		add(dir)
	}
	result := []resourceInput{}
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	var notes []string
	for note := range warnings {
		notes = append(notes, note)
	}
	sort.Strings(notes)
	return result, notes, nil
}

// The journal's proposed manifest retains newly adopted identities during an
// interrupted first sync, even if neither native file exists after the crash.
// Targets are subsequently reconstructed and validated against the root policy.
func conventionTracked(c Config) (map[string]Resource, error) {
	m, _, err := readManifest(c)
	if err != nil {
		return nil, err
	}
	pending, err := snapshot(pendingPath(c))
	if err != nil || pending == nil {
		return m.Resources, err
	}
	var pointer syncPending
	if err := decodeEnrollmentJSON(pending, &pointer); err != nil {
		return nil, err
	}
	if !transactionPattern.MatchString(pointer.Transaction) {
		return nil, fmt.Errorf("invalid convention recovery transaction")
	}
	stored, err := snapshot(filepath.Join(c.StateDir, "backups", pointer.Transaction, "journal.json"))
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, fmt.Errorf("convention recovery journal missing")
	}
	var journal Journal
	if err := decodeEnrollmentJSON(stored, &journal); err != nil {
		return nil, err
	}
	if journal.Version != 1 || journal.Operations == nil {
		return nil, fmt.Errorf("invalid convention recovery journal")
	}
	for _, op := range journal.Operations {
		if op.Label != "manifest" || op.File != manifestPath(c) {
			continue
		}
		var proposed Manifest
		if err := decodeEnrollmentJSON(op.After, &proposed); err != nil {
			return nil, err
		}
		if proposed.Version != 2 || proposed.Resources == nil || proposed.Files == nil {
			return nil, fmt.Errorf("invalid convention recovery manifest")
		}
		for id, r := range proposed.Resources {
			if old, ok := m.Resources[id]; ok && !sameResourceIdentity(old, r) {
				return nil, fmt.Errorf("convention recovery identity changed")
			}
			m.Resources[id] = r
		}
	}
	return m.Resources, nil
}

func validateConventionContent(c Config, item Item, value *Snapshot) error {
	if c.Conventions == nil || !(strings.HasPrefix(item.ID, conventionPrefix) || (featureResourceID(item.ID) && item.Kind == "portable-file")) || value == nil {
		return nil
	}
	data, err := snapshotBytes(value)
	if err != nil {
		return err
	}
	return checkInstructionImports(string(data))
}
