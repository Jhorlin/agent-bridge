package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var sides = []string{"shared", "claude", "codex"}
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var reserved = "|__proto__|constructor|prototype|toString|toLocaleString|valueOf|hasOwnProperty|isPrototypeOf|propertyIsEnumerable|__defineGetter__|__defineSetter__|__lookupGetter__|__lookupSetter__|"

type Resource struct {
	ID                string            `json:"id"`
	Kind              string            `json:"kind"`
	Scope             string            `json:"scope"`
	Paths             map[string]string `json:"paths"`
	Servers           []string          `json:"servers,omitempty"`
	Links             map[string]Link   `json:"links,omitempty"`
	AllowReformat     bool              `json:"allowReformat,omitempty"`
	CodexPluginLayout string            `json:"codexPluginLayout,omitempty"`
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
		ID                string          `json:"id"`
		Kind              string          `json:"kind"`
		Scope             string          `json:"scope"`
		Paths             orderedPaths    `json:"paths"`
		Servers           []string        `json:"servers,omitempty"`
		Links             map[string]Link `json:"links,omitempty"`
		AllowReformat     bool            `json:"allowReformat,omitempty"`
		CodexPluginLayout string          `json:"codexPluginLayout,omitempty"`
	}{r.ID, r.Kind, r.Scope, orderedPaths{r.Paths["shared"], r.Paths["claude"], r.Paths["codex"]}, r.Servers, r.Links, r.AllowReformat, r.CodexPluginLayout})
}

type Config struct {
	StateDir    string     `json:"stateDir"`
	Resources   []Resource `json:"resources"`
	ConfigFiles []string   `json:"configFiles"`
}
type resourceInput struct {
	ID                string            `json:"id"`
	Kind              string            `json:"kind"`
	Scope             string            `json:"scope"`
	Portable          bool              `json:"portable"`
	Claude            string            `json:"claude"`
	Codex             string            `json:"codex"`
	Servers           []string          `json:"servers,omitempty"`
	LinkTargets       map[string]string `json:"linkTargets,omitempty"`
	AllowReformat     bool              `json:"allowReformat,omitempty"`
	CodexPluginLayout string            `json:"codexPluginLayout,omitempty"`
}
type configInput struct {
	Version   int             `json:"version"`
	StateDir  string          `json:"stateDir"`
	Resources []resourceInput `json:"resources"`
	Extends   string          `json:"extends,omitempty"`
	Disable   []string        `json:"disable,omitempty"`
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
	var c Config
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return c, err
	}
	raw, configFiles, err := inherit(absolute, map[string]bool{})
	if err != nil {
		return c, err
	}
	if raw.Version != 1 || raw.StateDir == "" || raw.Resources == nil {
		return c, fmt.Errorf("expected version: 1, stateDir, and resources array")
	}
	c.StateDir = resolve(filepath.Dir(absolute), raw.StateDir)
	c.ConfigFiles = configFiles
	destinations := append([]string{c.StateDir}, configFiles...)
	ids := map[string]bool{}
	for _, r := range raw.Resources {
		if !safeID.MatchString(r.ID) || ids[r.ID] || strings.Contains(reserved, "|"+r.ID+"|") {
			return c, fmt.Errorf("resource IDs must be unique and path-safe")
		}
		ids[r.ID] = true
		if r.Kind != "portable-file" && r.Kind != "skill-directory" && r.Kind != "mcp-config" && r.Kind != "plugin-directory" {
			return c, fmt.Errorf("unsupported adapter: %s", r.Kind)
		}
		if (r.Kind == "skill-directory" || r.Kind == "plugin-directory") && !r.Portable {
			return c, fmt.Errorf("%s requires portable: true after reviewing tool compatibility", r.Kind)
		}
		if r.Scope != "global" && r.Scope != "project" {
			return c, fmt.Errorf("each resource needs global or project scope")
		}
		res := Resource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Paths: map[string]string{"shared": filepath.Join(c.StateDir, "shared", r.ID)}}
		if r.CodexPluginLayout != "" {
			if r.Kind != "plugin-directory" || r.CodexPluginLayout != "portable" {
				return c, fmt.Errorf("codexPluginLayout supports portable for plugin-directory only")
			}
			res.CodexPluginLayout = r.CodexPluginLayout
		}
		if r.Kind == "mcp-config" {
			if len(r.Servers) == 0 || !r.AllowReformat {
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
		c.Resources = append(c.Resources, res)
	}
	return c, nil
}

// Full-resource overrides only. Relative paths belong to their declaring file.
// The leaf config always owns its state directory, isolating project baselines.
func inherit(file string, stack map[string]bool) (configInput, []string, error) {
	var raw configInput
	if stack[file] || len(stack) >= 32 {
		return raw, nil, fmt.Errorf("config inheritance cycle or depth exceeded")
	}
	stack[file] = true
	defer delete(stack, file)
	if err := assertSafe(file); err != nil {
		return raw, nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return raw, nil, err
	}
	if err = strictJSON(data, &raw); err != nil {
		return raw, nil, err
	}
	if raw.Version != 1 || raw.StateDir == "" || raw.Resources == nil {
		return raw, nil, fmt.Errorf("expected version: 1, stateDir, and resources array")
	}
	files := []string{file}
	base := filepath.Dir(file)
	merged := []resourceInput{}
	if raw.Extends != "" {
		parent, pfiles, err := inherit(resolve(base, raw.Extends), stack)
		if err != nil {
			return raw, nil, err
		}
		merged = parent.Resources
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
