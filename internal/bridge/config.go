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
	ID    string            `json:"id"`
	Kind  string            `json:"kind"`
	Scope string            `json:"scope"`
	Paths map[string]string `json:"paths"`
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
		ID    string       `json:"id"`
		Kind  string       `json:"kind"`
		Scope string       `json:"scope"`
		Paths orderedPaths `json:"paths"`
	}{r.ID, r.Kind, r.Scope, orderedPaths{r.Paths["shared"], r.Paths["claude"], r.Paths["codex"]}})
}

type Config struct {
	StateDir  string
	Resources []Resource
}
type resourceInput struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Portable bool   `json:"portable"`
	Claude   string `json:"claude"`
	Codex    string `json:"codex"`
}
type configInput struct {
	Version   int             `json:"version"`
	StateDir  string          `json:"stateDir"`
	Resources []resourceInput `json:"resources"`
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
	data, err := os.ReadFile(absolute)
	if err != nil {
		return c, err
	}
	var raw configInput
	if err = json.Unmarshal(data, &raw); err != nil {
		return c, err
	}
	if raw.Version != 1 || raw.StateDir == "" || raw.Resources == nil {
		return c, fmt.Errorf("expected version: 1, stateDir, and resources array")
	}
	c.StateDir = resolve(filepath.Dir(absolute), raw.StateDir)
	destinations := []string{c.StateDir, absolute}
	ids := map[string]bool{}
	for _, r := range raw.Resources {
		if !safeID.MatchString(r.ID) || ids[r.ID] || strings.Contains(reserved, "|"+r.ID+"|") {
			return c, fmt.Errorf("resource IDs must be unique and path-safe")
		}
		ids[r.ID] = true
		if r.Kind != "portable-file" && r.Kind != "skill-directory" {
			return c, fmt.Errorf("unsupported adapter: %s", r.Kind)
		}
		if r.Kind == "skill-directory" && !r.Portable {
			return c, fmt.Errorf("skill-directory requires portable: true after reviewing tool compatibility")
		}
		if r.Scope != "global" && r.Scope != "project" {
			return c, fmt.Errorf("each resource needs global or project scope")
		}
		res := Resource{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Paths: map[string]string{"shared": filepath.Join(c.StateDir, "shared", r.ID)}}
		for _, side := range sides[1:] {
			name := r.Claude
			if side == "codex" {
				name = r.Codex
			}
			if name == "" || strings.HasPrefix(name, "~") {
				return c, fmt.Errorf("use explicit absolute or config-relative paths, not tilde paths")
			}
			dest := resolve(filepath.Dir(absolute), name)
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
