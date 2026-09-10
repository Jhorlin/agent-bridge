package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var sides = []string{"shared", "claude", "codex"}
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var reserved = "|__proto__|constructor|prototype|toString|toLocaleString|valueOf|hasOwnProperty|isPrototypeOf|propertyIsEnumerable|__defineGetter__|__defineSetter__|__lookupGetter__|__lookupSetter__|"

type Resource struct {
	ID                       string            `json:"id"`
	Kind                     string            `json:"kind"`
	Scope                    string            `json:"scope"`
	Paths                    map[string]string `json:"paths"`
	Servers                  []string          `json:"servers,omitempty"`
	Links                    map[string]Link   `json:"links,omitempty"`
	AllowReformat            bool              `json:"allowReformat,omitempty"`
	CodexPluginLayout        string            `json:"codexPluginLayout,omitempty"`
	PreserveCodexMCPPolicies bool              `json:"preserveCodexMCPPolicies,omitempty"`
	PreserveAgentSettings    bool              `json:"preserveAgentSettings,omitempty"`
	TranslateSkillInvocation bool              `json:"translateSkillInvocation,omitempty"`
	PreserveSkillSettings    bool              `json:"preserveSkillSettings,omitempty"`
	CodexAgentExports        map[string]string `json:"codexAgentExports,omitempty"`
	FileGuard                *FileGuardConfig  `json:"fileGuard,omitempty"`
	PreserveCommandSettings  bool              `json:"preserveCommandSettings,omitempty"`
}

type Link struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}

// Preserve the v0.2 Node engine's resource/path property order. That engine
// compares resource identity with JSON.stringify rather than semantic equality.
func (r Resource) MarshalJSON() ([]byte, error) {
	type orderedPaths struct {
		Shared string `json:"shared"`
		Claude string `json:"claude"`
		Codex  string `json:"codex"`
	}
	return json.Marshal(struct {
		ID                       string            `json:"id"`
		Kind                     string            `json:"kind"`
		Scope                    string            `json:"scope"`
		Paths                    orderedPaths      `json:"paths"`
		Servers                  []string          `json:"servers,omitempty"`
		Links                    map[string]Link   `json:"links,omitempty"`
		AllowReformat            bool              `json:"allowReformat,omitempty"`
		CodexPluginLayout        string            `json:"codexPluginLayout,omitempty"`
		PreserveCodexMCPPolicies bool              `json:"preserveCodexMCPPolicies,omitempty"`
		PreserveAgentSettings    bool              `json:"preserveAgentSettings,omitempty"`
		TranslateSkillInvocation bool              `json:"translateSkillInvocation,omitempty"`
		PreserveSkillSettings    bool              `json:"preserveSkillSettings,omitempty"`
		CodexAgentExports        map[string]string `json:"codexAgentExports,omitempty"`
		FileGuard                *FileGuardConfig  `json:"fileGuard,omitempty"`
		PreserveCommandSettings  bool              `json:"preserveCommandSettings,omitempty"`
	}{r.ID, r.Kind, r.Scope, orderedPaths{r.Paths["shared"], r.Paths["claude"], r.Paths["codex"]}, r.Servers, r.Links, r.AllowReformat, r.CodexPluginLayout, r.PreserveCodexMCPPolicies, r.PreserveAgentSettings, r.TranslateSkillInvocation, r.PreserveSkillSettings, r.CodexAgentExports, r.FileGuard, r.PreserveCommandSettings})
}

type Config struct {
	Conventions        *Conventions `json:"conventions,omitempty"`
	ConventionWarnings []string     `json:"conventionWarnings,omitempty"`
	CoordinationDir    string       `json:"coordinationDir,omitempty"`
	StateDir           string       `json:"stateDir"`
	Resources          []Resource   `json:"resources"`
	ConfigFiles        []string     `json:"configFiles"`
}
type resourceInput struct {
	ID                       string            `json:"id"`
	Kind                     string            `json:"kind"`
	Scope                    string            `json:"scope"`
	Portable                 bool              `json:"portable"`
	Claude                   string            `json:"claude"`
	Codex                    string            `json:"codex"`
	Servers                  []string          `json:"servers,omitempty"`
	LinkTargets              map[string]string `json:"linkTargets,omitempty"`
	AllowReformat            bool              `json:"allowReformat,omitempty"`
	CodexPluginLayout        string            `json:"codexPluginLayout,omitempty"`
	PreserveCodexMCPPolicies bool              `json:"preserveCodexMCPPolicies,omitempty"`
	PreserveAgentSettings    bool              `json:"preserveAgentSettings,omitempty"`
	TranslateSkillInvocation bool              `json:"translateSkillInvocation,omitempty"`
	PreserveSkillSettings    bool              `json:"preserveSkillSettings,omitempty"`
	CodexAgentExports        map[string]string `json:"codexAgentExports,omitempty"`
	FileGuard                *FileGuardConfig  `json:"fileGuard,omitempty"`
	PreserveCommandSettings  bool              `json:"preserveCommandSettings,omitempty"`
}
type configInput struct {
	Conventions     *Conventions    `json:"conventions,omitempty"`
	CoordinationDir string          `json:"coordinationDir,omitempty"`
	Version         int             `json:"version"`
	StateDir        string          `json:"stateDir"`
	Resources       []resourceInput `json:"resources"`
	Extends         string          `json:"extends,omitempty"`
	Disable         []string        `json:"disable,omitempty"`
}

func inside(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}
func resolve(base, name string) string {
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(base, name)
}

func LoadConfig(filename string) (Config, error) {
	return loadConfig(filename, false)
}

func loadConfig(filename string, audit bool) (Config, error) {
	return loadConfigMode(filename, audit, true)
}

func loadConfigMode(filename string, audit, discover bool) (Config, error) {
	var c Config
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return c, err
	}
	raw, configFiles, err := inherit(absolute, map[string]bool{}, audit)
	if err != nil {
		return c, err
	}
	if raw.Version != 1 || raw.StateDir == "" || raw.Resources == nil {
		return c, fmt.Errorf("expected version: 1, stateDir, and resources array")
	}
	c.StateDir = resolve(filepath.Dir(absolute), raw.StateDir)
	c.ConfigFiles = configFiles
	c.CoordinationDir = raw.CoordinationDir
	c.Conventions = raw.Conventions
	if c.Conventions == nil || !discover {
		for _, r := range raw.Resources {
			if automaticResource(r.ID) {
				return c, fmt.Errorf("automatic resource IDs require a convention policy")
			}
		}
	}
	if c.Conventions != nil && discover {
		var discovered []resourceInput
		discovered, c.ConventionWarnings, err = discoverConventions(c, raw.Resources)
		if err != nil {
			return c, err
		}
		raw.Resources = append(raw.Resources, discovered...)
	}
	destinations := append([]string{c.StateDir}, configFiles...)
	if raw.CoordinationDir != "" {
		c.CoordinationDir = raw.CoordinationDir
		if err := assertSafe(c.CoordinationDir); err != nil {
			return c, err
		}
		for _, other := range destinations {
			a, b := strings.ToLower(other), strings.ToLower(c.CoordinationDir)
			if inside(a, b) || inside(b, a) {
				return c, fmt.Errorf("coordinationDir must not overlap stateDir or config")
			}
		}
		destinations = append(destinations, c.CoordinationDir)
	}
	ids := map[string]bool{}
	for _, r := range raw.Resources {
		if !safeID.MatchString(r.ID) || ids[r.ID] || strings.Contains(reserved, "|"+r.ID+"|") {
			return c, fmt.Errorf("resource IDs must be unique and path-safe")
		}
		ids[r.ID] = true
		if r.Kind != "portable-file" && r.Kind != "skill-directory" && r.Kind != "mcp-config" && r.Kind != "plugin-directory" && r.Kind != "instruction-file" && r.Kind != "instruction-set" && r.Kind != "agent-file" && r.Kind != "hook-config" && r.Kind != "file-guard-config" {
			return c, fmt.Errorf("unsupported adapter: %s", r.Kind)
		}
		if (r.Kind == "skill-directory" || r.Kind == "plugin-directory" || r.Kind == "instruction-file" || r.Kind == "instruction-set" || r.Kind == "agent-file" || r.Kind == "hook-config" || r.Kind == "file-guard-config") && !r.Portable {
			return c, fmt.Errorf("%s requires portable: true after reviewing tool compatibility", r.Kind)
		}
		if r.Scope != "global" && r.Scope != "project" {
			return c, fmt.Errorf("each resource needs global or project scope")
		}
		res := Resource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Paths: map[string]string{"shared": filepath.Join(c.StateDir, "shared", r.ID)}}
		if (r.Kind == "file-guard-config") != (r.FileGuard != nil) {
			return c, fmt.Errorf("fileGuard is required exclusively for file-guard-config")
		}
		res.FileGuard = r.FileGuard
		if r.PreserveCommandSettings {
			if r.Kind != "plugin-directory" || !r.AllowReformat {
				return c, fmt.Errorf("preserveCommandSettings requires a plugin with allowReformat")
			}
			res.PreserveCommandSettings = true
		}
		if r.CodexAgentExports != nil {
			if r.Kind != "plugin-directory" || !r.AllowReformat || len(r.CodexAgentExports) == 0 || len(r.ID) > 64 || len(r.LinkTargets) != 0 {
				return c, fmt.Errorf("codexAgentExports requires an unlinked plugin, explicit exports and allowReformat")
			}
			res.CodexAgentExports = r.CodexAgentExports
		}
		if r.TranslateSkillInvocation {
			if r.Kind != "skill-directory" || !r.AllowReformat {
				return c, fmt.Errorf("translateSkillInvocation requires a strict skill-directory")
			}
			res.TranslateSkillInvocation = true
		}
		if r.PreserveSkillSettings {
			if r.Kind != "skill-directory" || !r.TranslateSkillInvocation || !r.AllowReformat {
				return c, fmt.Errorf("preserveSkillSettings requires skill invocation translation")
			}
			res.PreserveSkillSettings = true
		}
		if r.PreserveAgentSettings {
			if r.Kind != "agent-file" && !(r.Kind == "plugin-directory" && (len(r.CodexAgentExports) > 0 || featureResourceID(r.ID))) {
				return c, fmt.Errorf("preserveAgentSettings requires agent-file or explicit plugin agent exports")
			}
			res.PreserveAgentSettings = true
		}
		if r.PreserveCodexMCPPolicies {
			if r.Kind != "mcp-config" {
				return c, fmt.Errorf("preserveCodexMCPPolicies requires mcp-config")
			}
			res.PreserveCodexMCPPolicies = true
		}
		if r.CodexPluginLayout != "" {
			if r.Kind != "plugin-directory" || r.CodexPluginLayout != "portable" {
				return c, fmt.Errorf("codexPluginLayout supports portable for plugin-directory only")
			}
			res.CodexPluginLayout = r.CodexPluginLayout
		}
		if r.Kind == "mcp-config" || (r.Kind == "plugin-directory" && len(r.Servers) > 0) {
			if (len(r.Servers) == 0 && !featureResourceID(r.ID)) || !r.AllowReformat {
				return c, fmt.Errorf("mcp-config requires a servers allowlist and allowReformat: true")
			}
			seen := map[string]bool{}
			for _, name := range r.Servers {
				if !safeID.MatchString(name) || seen[name] || strings.Contains(reserved, "|"+name+"|") {
					return c, fmt.Errorf("invalid or duplicate MCP server name")
				}
				seen[name] = true
			}
			res.Servers = r.Servers
			res.AllowReformat = true
			if r.Kind == "plugin-directory" && r.CodexPluginLayout == "portable" {
				return c, fmt.Errorf("bundled MCP currently requires the compatibility plugin layout")
			}
		} else if r.Kind == "plugin-directory" && (len(r.CodexAgentExports) > 0 || featureResourceID(r.ID) || r.PreserveCommandSettings) {
			res.AllowReformat = true
		} else if r.Kind == "skill-directory" {
			if len(r.Servers) > 0 {
				return c, fmt.Errorf("skill-directory does not accept servers")
			}
			res.AllowReformat = r.AllowReformat
		} else if r.Kind == "agent-file" || r.Kind == "hook-config" || r.Kind == "file-guard-config" {
			if !r.AllowReformat || len(r.Servers) > 0 {
				return c, fmt.Errorf("agent and hook adapters require allowReformat and do not accept servers")
			}
			res.AllowReformat = true
		} else if len(r.Servers) > 0 || r.AllowReformat {
			return c, fmt.Errorf("MCP options require mcp-config")
		}
		for side := range r.LinkTargets {
			if side != "claude" && side != "codex" {
				return c, fmt.Errorf("only native paths may have linkTargets")
			}
		}
		for _, side := range sides[1:] {
			name := r.Claude
			if side == "codex" {
				name = r.Codex
			}
			if name == "" || strings.HasPrefix(name, "~") {
				return c, fmt.Errorf("use explicit absolute or config-relative paths, not tilde paths")
			}
			dest := resolve(filepath.Dir(absolute), name)
			if target, ok := r.LinkTargets[side]; ok {
				if target == "" || strings.HasPrefix(target, "~") {
					return c, fmt.Errorf("linkTargets require explicit paths")
				}
				link := Link{dest, resolve(filepath.Dir(absolute), target)}
				if err := validateLink(link); err != nil {
					return c, err
				}
				if res.Links == nil {
					res.Links = map[string]Link{}
				}
				res.Links[side] = link
				dest = link.Target
			}
			for _, other := range destinations {
				a, b := strings.ToLower(other), strings.ToLower(dest)
				if inside(a, b) || inside(b, a) {
					return c, fmt.Errorf("resource paths must not overlap each other, stateDir, or config")
				}
			}
			destinations = append(destinations, dest)
			res.Paths[side] = dest
		}
		if r.Kind == "instruction-set" {
			if r.Scope != "project" || len(r.LinkTargets) != 0 || filepath.Base(res.Paths["claude"]) != "CLAUDE.md" || filepath.Base(res.Paths["codex"]) != "AGENTS.md" || filepath.Dir(res.Paths["claude"]) != filepath.Dir(res.Paths["codex"]) {
				return c, fmt.Errorf("instruction-set requires an unlinked project CLAUDE.md/AGENTS.md pair in the same directory")
			}
			dest := InstructionCompanionPath(res)
			if err := assertSafe(dest); err != nil {
				return c, err
			}
			for _, other := range destinations {
				a, b := strings.ToLower(other), strings.ToLower(dest)
				if inside(a, b) || inside(b, a) {
					return c, fmt.Errorf("instruction companion overlaps a managed path")
				}
			}
			destinations = append(destinations, dest)
		}
		exports := []string{}
		for name := range res.CodexAgentExports {
			exports = append(exports, name)
		}
		sort.Strings(exports)
		for _, name := range exports {
			dest := res.CodexAgentExports[name]
			if !skillName.MatchString(name) || len(name) > 64 || !filepath.IsAbs(dest) || filepath.Clean(dest) != dest || filepath.Base(dest) != exportedAgentName(r.ID, name)+".toml" {
				return c, fmt.Errorf("agent exports require explicit paths with namespaced TOML filenames and kebab-case source names")
			}
			if err := assertSafe(dest); err != nil {
				return c, err
			}
			for _, other := range destinations {
				a, b := strings.ToLower(other), strings.ToLower(dest)
				if inside(a, b) || inside(b, a) {
					return c, fmt.Errorf("agent export paths must not overlap managed paths")
				}
			}
			destinations = append(destinations, dest)
		}
		c.Resources = append(c.Resources, res)
	}
	for _, r := range c.Resources {
		if err := validateFileGuardConfig(r, destinations); err != nil {
			return c, err
		}
	}
	return c, nil
}

// Full-resource overrides only. Relative paths belong to their declaring file.
// The leaf config always owns its state directory, isolating project baselines.
func inherit(file string, stack map[string]bool, audit bool) (configInput, []string, error) {
	var raw configInput
	if stack[file] || len(stack) >= 32 {
		return raw, nil, fmt.Errorf("config inheritance cycle or depth exceeded")
	}
	stack[file] = true
	defer delete(stack, file)
	if err := assertSafe(file); err != nil {
		return raw, nil, err
	}
	var data []byte
	var err error
	if audit {
		// Audit decodes exactly the safe snapshot it validates, not a second read.
		var rawFile *Snapshot
		rawFile, err = snapshot(file)
		if err == nil && rawFile == nil {
			err = fmt.Errorf("audit profile is missing")
		}
		if err == nil {
			data, err = base64.StdEncoding.DecodeString(rawFile.Data)
		}
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return raw, nil, err
	}
	if err = strictJSON(data, &raw); err != nil {
		return raw, nil, err
	}
	if audit {
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(&raw); err != nil {
			return raw, nil, fmt.Errorf("invalid or unknown audit profile fields")
		}
	}
	if raw.Version != 1 || raw.StateDir == "" || raw.Resources == nil {
		return raw, nil, fmt.Errorf("expected version: 1, stateDir, and resources array")
	}
	files := []string{file}
	base := filepath.Dir(file)
	if raw.Conventions != nil {
		if err := raw.Conventions.resolve(base); err != nil {
			return raw, nil, err
		}
	}
	if raw.CoordinationDir != "" {
		if strings.HasPrefix(raw.CoordinationDir, "~") {
			return raw, nil, fmt.Errorf("coordinationDir requires an explicit path")
		}
		raw.CoordinationDir = resolve(base, raw.CoordinationDir)
	}
	merged := []resourceInput{}
	if raw.Extends != "" {
		parent, pfiles, err := inherit(resolve(base, raw.Extends), stack, audit)
		if err != nil {
			return raw, nil, err
		}
		merged = parent.Resources
		if parent.Conventions != nil {
			return raw, nil, fmt.Errorf("conventions must be declared in the leaf profile, not an inherited profile")
		}
		if raw.CoordinationDir == "" {
			raw.CoordinationDir = parent.CoordinationDir
		}
		files = append(files, pfiles...)
	}
	seen := map[string]bool{}
	for _, r := range raw.Resources {
		if seen[r.ID] {
			return raw, nil, fmt.Errorf("resource IDs must be unique and path-safe")
		}
		seen[r.ID] = true
	}
	for _, r := range raw.Resources {
		if r.Claude != "" && !strings.HasPrefix(r.Claude, "~") {
			r.Claude = resolve(base, r.Claude)
		}
		if r.Codex != "" && !strings.HasPrefix(r.Codex, "~") {
			r.Codex = resolve(base, r.Codex)
		}
		for side, target := range r.LinkTargets {
			if target != "" && !strings.HasPrefix(target, "~") {
				r.LinkTargets[side] = resolve(base, target)
			}
		}
		for name, target := range r.CodexAgentExports {
			if target != "" && !strings.HasPrefix(target, "~") {
				r.CodexAgentExports[name] = resolve(base, target)
			}
		}
		found := false
		for i, old := range merged {
			if old.ID == r.ID {
				merged[i] = r
				found = true
				break
			}
		}
		if !found {
			merged = append(merged, r)
		}
	}
	disabled := map[string]bool{}
	for _, id := range raw.Disable {
		if seen[id] {
			return raw, nil, fmt.Errorf("cannot declare and disable the same resource")
		}
		found := false
		for _, r := range merged {
			if r.ID == id {
				found = true
			}
		}
		if !found {
			return raw, nil, fmt.Errorf("disabled resource does not exist")
		}
		disabled[id] = true
	}
	raw.Resources = []resourceInput{}
	for _, r := range merged {
		if !disabled[r.ID] {
			raw.Resources = append(raw.Resources, r)
		}
	}
	return raw, files, nil
}

func validateLink(link Link) error {
	if err := assertSafe(filepath.Dir(link.Path)); err != nil {
		return err
	}
	target, err := os.Readlink(link.Path)
	if err != nil {
		return fmt.Errorf("pinned native path must be a symlink: %s", link.Path)
	}
	if resolve(filepath.Dir(link.Path), target) != link.Target {
		return fmt.Errorf("symlink target changed: %s", link.Path)
	}
	if err = assertSafe(link.Target); err != nil {
		return err
	}
	if _, err = os.Lstat(link.Target); err != nil {
		return fmt.Errorf("pinned symlink target is missing")
	}
	return nil
}
func validateLinks(c Config) error {
	for _, r := range c.Resources {
		for _, link := range r.Links {
			if err := validateLink(link); err != nil {
				return err
			}
		}
	}
	return nil
}
