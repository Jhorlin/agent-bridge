package bridge

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

var conventionFeatures = []string{"instructions", "skills", "agents", "hooks", "mcp", "plugins"}

const featurePrefix = "auto-feature-"

func automaticResource(id string) bool {
	return strings.HasPrefix(id, conventionPrefix) || strings.HasPrefix(id, featurePrefix)
}
func featureResourceID(id string) bool { return strings.HasPrefix(id, featurePrefix) }
func (p *Conventions) scope() string {
	if p.Scope == "global" {
		return "global"
	}
	return "project"
}
func (p *Conventions) enabled(feature string) bool {
	if p.Features == nil {
		return feature == "instructions"
	} // Preserve existing instruction-only profiles.
	return hasField(p.Features, feature)
}

func featureEntry(c Config, feature, base, name string) resourceInput {
	rel, _ := filepath.Rel(c.Conventions.Root, base)
	hash := sha256.Sum256([]byte(c.Conventions.scope() + "/" + feature + "/" + filepath.ToSlash(rel) + "/" + name))
	r := resourceInput{ID: fmt.Sprintf("%s%s-%x", featurePrefix, feature, hash[:16]), Scope: c.Conventions.scope()}
	switch feature {
	case "instructions":
		r.Kind = "portable-file"
		r.Claude = filepath.Join(base, ".claude", "CLAUDE.md")
		r.Codex = filepath.Join(base, ".codex", "AGENTS.md")
	case "skills":
		r.Kind = "skill-directory"
		r.Portable = true
		r.AllowReformat = true
		r.TranslateSkillInvocation = true
		r.PreserveSkillSettings = c.Conventions.PreserveSkillSettings
		r.Claude = filepath.Join(base, ".claude", "skills", name)
		r.Codex = filepath.Join(base, ".agents", "skills", name)
	case "agents":
		r.Kind = "agent-file"
		r.Portable = true
		r.AllowReformat = true
		r.PreserveAgentSettings = true
		r.Claude = filepath.Join(base, ".claude", "agents", name+".md")
		r.Codex = filepath.Join(base, ".codex", "agents", name+".toml")
	case "hooks":
		r.Kind = "hook-config"
		r.Portable = true
		r.AllowReformat = true
		r.Claude = filepath.Join(base, ".claude", "settings.json")
		r.Codex = filepath.Join(base, ".codex", "hooks.json")
	case "mcp":
		r.Kind = "mcp-config"
		r.AllowReformat = true
		r.PreserveCodexMCPPolicies = true
		r.Claude = filepath.Join(base, ".mcp.json")
		if r.Scope == "global" {
			r.Claude = filepath.Join(base, ".claude.json")
		}
		r.Codex = filepath.Join(base, ".codex", "config.toml")
	case "plugins":
		r.Kind = "plugin-directory"
		r.Portable = true
		r.AllowReformat = true
		r.PreserveAgentSettings = true
		r.Claude = filepath.Join(base, ".agent-bridge-plugins", "claude", name)
		r.Codex = filepath.Join(base, ".agent-bridge-plugins", "codex", name)
	}
	return r
}

func resolvedFeature(c Config, r resourceInput) Resource {
	return Resource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Paths: map[string]string{"claude": r.Claude, "codex": r.Codex, "shared": filepath.Join(c.StateDir, "shared", r.ID)}, Servers: r.Servers, AllowReformat: r.AllowReformat, PreserveCodexMCPPolicies: r.PreserveCodexMCPPolicies, PreserveAgentSettings: r.PreserveAgentSettings, TranslateSkillInvocation: r.TranslateSkillInvocation, PreserveSkillSettings: r.PreserveSkillSettings, CodexAgentExports: r.CodexAgentExports, CodexPluginLayout: r.CodexPluginLayout}
}

// Auto-managed collections may grow, but existing members never silently vanish
// or move. Native paths, adapter choices and host-local policy modes stay pinned.
func sameResourceIdentity(previous, current Resource) bool {
	if featureResourceID(current.ID) && previous.ID == current.ID {
		// Opting in only separates new native-local fields; the previously
		// accepted portable projection is unchanged. Downgrades remain blocked.
		if current.Kind == "skill-directory" && current.TranslateSkillInvocation && current.PreserveSkillSettings && !previous.PreserveSkillSettings {
			current.PreserveSkillSettings = false
		}
		for _, name := range previous.Servers {
			if !hasField(current.Servers, name) {
				return false
			}
		}
		for name, path := range previous.CodexAgentExports {
			if current.CodexAgentExports[name] != path {
				return false
			}
		}
		current.Servers = previous.Servers
		current.CodexAgentExports = previous.CodexAgentExports
	}
	return reflect.DeepEqual(previous, current)
}

func discoverConventions(c Config, explicit []resourceInput) ([]resourceInput, []string, error) {
	for _, r := range explicit {
		if automaticResource(r.ID) {
			return nil, nil, fmt.Errorf("automatic resource IDs are reserved for convention discovery")
		}
	}
	result := []resourceInput{}
	warnings := []string{}
	if c.Conventions.scope() == "project" && c.Conventions.enabled("instructions") {
		var err error
		result, warnings, err = discoverInstructionConventions(c, explicit)
		if err != nil {
			return nil, nil, err
		}
	}
	more, notes, err := discoverFeatureConventions(c, explicit)
	if err != nil {
		return nil, nil, err
	}
	result = append(result, more...)
	warnings = append(warnings, notes...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	sort.Strings(warnings)
	return result, warnings, nil
}

// Global discovery deliberately never walks a home directory or its projects.
func conventionFeatureDirs(c Config) ([]string, error) {
	p := c.Conventions
	if err := assertSafe(p.Root); err != nil {
		return nil, err
	}
	info, err := os.Stat(p.Root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("convention root must be an existing safe directory")
	}
	if conventionStorage(c, p.Root) {
		return nil, fmt.Errorf("convention root overlaps private state")
	}
	if p.scope() == "global" {
		return []string{p.Root}, nil
	}
	// Walk the tree for new components, but only probe native collections at
	// candidate bases. Probing every feature under every source directory adds
	// many redundant ancestor safety checks in large monorepos. Missing managed
	// collections are independently restored from their validated recipes below.
	bases := map[string]bool{p.Root: true}
	err = filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path != p.Root {
			switch strings.ToLower(d.Name()) {
			case ".claude", ".codex", ".agents", ".mcp.json", ".agent-bridge-plugins":
				// Include symlinks and wrong file types so ordinary native-path
				// validation still rejects them rather than hiding unsafe inputs.
				bases[filepath.Dir(path)] = true
			}
		}
		if !d.IsDir() {
			return nil
		}
		if p.excluded(path) || conventionStorage(c, path) {
			return filepath.SkipDir
		}
		if path != p.Root {
			if strings.HasPrefix(d.Name(), ".") || conventionSkippedDirs[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			if _, e := os.Lstat(filepath.Join(path, ".git")); e == nil {
				return filepath.SkipDir
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		return nil
	})
	var dirs []string
	for base := range bases {
		dirs = append(dirs, base)
	}
	sort.Strings(dirs)
	return dirs, err
}

func conventionNames(root, suffix string) ([]string, error) {
	return conventionNamesFiltered(root, suffix, nil)
}

func conventionNamesFiltered(root, suffix string, skip func(string) bool) ([]string, error) {
	if err := assertSafe(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if skip != nil && skip(strings.TrimSuffix(entry.Name(), suffix)) {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("convention components cannot be symlinks")
		}
		name := entry.Name()
		if suffix == "" {
			if entry.Type().IsRegular() && (name == "README.md" || name == "LICENSE" || name == "LICENSE.md") {
				continue // Collection documentation is not an executable component.
			}
			if !entry.IsDir() {
				return nil, fmt.Errorf("conventional component collection requires named directories")
			}
		} else {
			if entry.IsDir() || !strings.HasSuffix(name, suffix) {
				return nil, fmt.Errorf("unsupported file in conventional agent collection")
			}
			name = strings.TrimSuffix(name, suffix)
		}
		if suffix == ".toml" && strings.HasPrefix(name, "bridge-"+featurePrefix) {
			continue
		}
		if !skillName.MatchString(name) || len(name) > 64 {
			return nil, fmt.Errorf("convention component requires a kebab-case name of at most 64 characters")
		}
		names = append(names, name)
	}
	return names, nil
}

func componentSnapshot(path string) (*Snapshot, error) { return snapshot(path) }

func mcpNames(path, side string) ([]string, bool, error) {
	raw, err := componentSnapshot(path)
	if err != nil || raw == nil {
		return nil, false, err
	}
	doc, err := document(side, raw)
	if err != nil {
		return nil, false, err
	}
	key := "mcpServers"
	if side == "codex" {
		key = "mcp_servers"
	}
	value, ok := doc[key]
	if !ok {
		return nil, false, nil
	}
	entries, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("conventional MCP servers must be an object")
	}
	var names []string
	for name := range entries {
		if !safeID.MatchString(name) || strings.Contains(reserved, "|"+name+"|") {
			return nil, false, fmt.Errorf("unsafe conventional MCP server name")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true, nil
}

func unionNames(a, b []string) []string {
	set := map[string]bool{}
	for _, n := range append(append([]string{}, a...), b...) {
		set[n] = true
	}
	var out []string
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func discoverFeatureConventions(c Config, explicit []resourceInput) ([]resourceInput, []string, error) {
	dirs, err := conventionFeatureDirs(c)
	if err != nil {
		return nil, nil, err
	}
	p := c.Conventions
	entries := map[string]resourceInput{}
	var warnings []string
	if p.scope() == "global" && p.enabled("mcp") {
		warnings = append(warnings, "Global MCP reads the mixed Claude user file; only top-level server definitions are synchronized. Account state, per-project entries and authentication remain host-local; private recovery backups may contain the original native file.")
	}
	explicitPair := func(r resourceInput) bool {
		for _, old := range explicit {
			if old.Claude == r.Claude && old.Codex == r.Codex {
				return true
			}
		}
		return false
	}
	add := func(r resourceInput) {
		if p.excluded(r.Claude) || p.excluded(r.Codex) || explicitPair(r) {
			return
		}
		entries[r.ID] = r
	}
	tracked, err := conventionTracked(c)
	if err != nil {
		return nil, nil, err
	}
	// Retain missing collections using a reconstructed, validated recipe.
	for _, old := range tracked {
		if !featureResourceID(old.ID) {
			continue
		}
		r, base, feature, err := restoreFeatureRecipe(c, old)
		if err != nil {
			return nil, nil, err
		}
		if !p.enabled(feature) || p.excluded(r.Claude) || p.excluded(r.Codex) {
			continue
		}
		if err := validateFeatureBase(c, base); err != nil {
			return nil, nil, err
		}
		add(r)
	}
	for _, base := range dirs {
		if _, e := os.Lstat(filepath.Join(base, ".claude", "rules")); e == nil {
			warnings = append(warnings, "Claude scoped rules are not translated; review applicable rule files independently in Codex. Synchronization does not establish instruction or policy equivalence.")
		} else if !os.IsNotExist(e) {
			return nil, nil, e
		}
		for _, feature := range conventionFeatures {
			if !p.enabled(feature) || (feature == "instructions" && p.scope() != "global") {
				continue
			}
			if feature == "skills" || feature == "agents" || feature == "plugins" {
				template := featureEntry(c, feature, base, "placeholder")
				a, b := filepath.Dir(template.Claude), filepath.Dir(template.Codex)
				if p.excluded(a) || p.excluded(b) {
					continue
				}
				sa, sb := "", ""
				if feature == "agents" {
					sa = ".md"
					sb = ".toml"
				}
				skip := func(name string) bool {
					r := featureEntry(c, feature, base, name)
					return p.excluded(r.Claude) || p.excluded(r.Codex) || explicitPair(r)
				}
				left, e := conventionNamesFiltered(a, sa, skip)
				if e != nil {
					return nil, nil, e
				}
				right, e := conventionNamesFiltered(b, sb, skip)
				if e != nil {
					return nil, nil, e
				}
				for _, name := range unionNames(left, right) {
					// Plugin exports are managed through their package, not adopted as
					// unrelated standalone agents when they appear on a later scan.
					if feature == "agents" && strings.HasPrefix(name, "bridge-"+featurePrefix) {
						continue
					}
					r := featureEntry(c, feature, base, name)
					if skip(name) {
						continue
					}
					if old, ok := entries[r.ID]; ok {
						r.Servers = old.Servers
						r.CodexAgentExports = old.CodexAgentExports
						r.CodexPluginLayout = old.CodexPluginLayout
					}
					if feature == "plugins" {
						if err := populatePluginConvention(c, base, &r); err != nil {
							return nil, nil, err
						}
					}
					add(r)
				}
				continue
			}
			r := featureEntry(c, feature, base, "")
			if p.excluded(r.Claude) || p.excluded(r.Codex) || explicitPair(r) {
				continue
			}
			if feature == "instructions" {
				override := filepath.Join(base, ".codex", "AGENTS.override.md")
				if !p.excluded(override) {
					if raw, e := snapshot(override); e != nil {
						return nil, nil, e
					} else if raw != nil {
						return nil, nil, fmt.Errorf("global AGENTS.override.md shadows conventional instructions; explicitly exclude or handle it")
					}
				}
			}
			present := false
			for _, side := range []string{"claude", "codex"} {
				path := r.Claude
				if side == "codex" {
					path = r.Codex
				}
				if feature == "mcp" {
					names, exists, e := mcpNames(path, side)
					if e != nil {
						return nil, nil, e
					}
					r.Servers = unionNames(r.Servers, names)
					present = present || exists
					continue
				}
				raw, e := componentSnapshot(path)
				if e != nil {
					return nil, nil, e
				}
				if raw == nil {
					continue
				}
				if feature == "hooks" {
					doc, e := document("claude", raw)
					if e != nil {
						return nil, nil, e
					}
					if value, ok := doc["hooks"]; ok {
						hooks, valid := value.(map[string]any)
						if !valid {
							return nil, nil, fmt.Errorf("conventional hooks must be an object")
						}
						present = present || len(hooks) > 0
					}
				} else {
					present = true
				}
			}
			if old, ok := entries[r.ID]; ok {
				r.Servers = unionNames(r.Servers, old.Servers)
				present = true
			}
			if present {
				add(r)
			}
		}
		if p.enabled("plugins") {
			for _, host := range []string{".claude", ".codex"} {
				if _, e := os.Lstat(filepath.Join(base, host, "plugins")); e == nil {
					warnings = append(warnings, "Installed plugin caches and enablement are host-managed; conventions synchronize authoring packages, not installation or trust.")
					break
				}
			}
		}
	}
	var result []resourceInput
	for _, r := range entries {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, unionNames(warnings, nil), nil
}

func populatePluginConvention(c Config, base string, r *resourceInput) error {
	portable, err := snapshot(filepath.Join(r.Codex, "plugin.json"))
	if err != nil {
		return err
	}
	compat, err := snapshot(filepath.Join(r.Codex, ".codex-plugin", "plugin.json"))
	if err != nil {
		return err
	}
	if portable != nil && compat != nil {
		return fmt.Errorf("ambiguous Codex plugin manifest layouts")
	}
	if portable != nil {
		r.CodexPluginLayout = "portable"
	}
	for _, root := range []string{r.Claude, r.Codex} {
		names, _, err := mcpNames(filepath.Join(root, ".mcp.json"), "claude")
		if err != nil {
			return err
		}
		r.Servers = unionNames(r.Servers, names)
	}
	names, err := conventionNames(filepath.Join(r.Claude, "agents"), ".md")
	if err != nil {
		return err
	}
	for _, name := range names {
		if r.CodexAgentExports == nil {
			r.CodexAgentExports = map[string]string{}
		}
		r.CodexAgentExports[name] = filepath.Join(base, ".codex", "agents", exportedAgentName(r.ID, name)+".toml")
	}
	return nil
}

func restoreFeatureRecipe(c Config, old Resource) (resourceInput, string, string, error) {
	var r resourceInput
	claude := old.Paths["claude"]
	base, name, feature := "", "", ""
	switch old.Kind {
	case "portable-file":
		feature = "instructions"
		base = filepath.Dir(filepath.Dir(claude))
	case "skill-directory":
		feature = "skills"
		name = filepath.Base(claude)
		base = filepath.Dir(filepath.Dir(filepath.Dir(claude)))
	case "agent-file":
		feature = "agents"
		name = strings.TrimSuffix(filepath.Base(claude), ".md")
		base = filepath.Dir(filepath.Dir(filepath.Dir(claude)))
	case "hook-config":
		feature = "hooks"
		base = filepath.Dir(filepath.Dir(claude))
	case "mcp-config":
		feature = "mcp"
		base = filepath.Dir(claude)
	case "plugin-directory":
		feature = "plugins"
		name = filepath.Base(claude)
		base = filepath.Dir(filepath.Dir(filepath.Dir(claude)))
	default:
		return r, "", "", fmt.Errorf("invalid conventional adapter identity")
	}
	if !inside(c.Conventions.Root, base) || (c.Conventions.scope() == "global" && base != c.Conventions.Root) {
		return r, "", "", fmt.Errorf("tracked convention root changed")
	}
	if name != "" && (!skillName.MatchString(name) || len(name) > 64) {
		return r, "", "", fmt.Errorf("invalid tracked convention name")
	}
	r = featureEntry(c, feature, base, name)
	r.Servers = old.Servers
	r.CodexPluginLayout = old.CodexPluginLayout
	for name := range old.CodexAgentExports {
		if feature != "plugins" || !skillName.MatchString(name) || len(name) > 64 {
			return r, "", "", fmt.Errorf("invalid tracked convention export")
		}
		if r.CodexAgentExports == nil {
			r.CodexAgentExports = map[string]string{}
		}
		r.CodexAgentExports[name] = filepath.Join(base, ".codex", "agents", exportedAgentName(r.ID, name)+".toml")
	}
	if !sameResourceIdentity(old, resolvedFeature(c, r)) {
		return r, "", "", fmt.Errorf("invalid tracked convention identity")
	}
	return r, base, feature, nil
}

func validateFeatureBase(c Config, base string) error {
	for path := base; path != c.Conventions.Root; path = filepath.Dir(path) {
		if strings.HasPrefix(filepath.Base(path), ".") || conventionSkippedDirs[strings.ToLower(filepath.Base(path))] || conventionStorage(c, path) {
			return fmt.Errorf("tracked feature crosses an excluded boundary")
		}
		if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
			return fmt.Errorf("tracked feature crosses a nested repository boundary")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
