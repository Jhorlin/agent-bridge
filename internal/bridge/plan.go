package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

type Manifest struct {
	Version   int                 `json:"version"`
	Resources map[string]Resource `json:"resources"`
	Files     map[string]string   `json:"files"`
}
type Item struct {
	Resource
	Key, Relative    string
	Values           map[string]*Snapshot
	Content          *Snapshot
	Digest, Conflict string
	Writes           []string
	Adapter          string
}
type PlanResult struct {
	Items          []Item
	Manifest       Manifest
	ManifestBefore *Snapshot
}

// Observation identifies all raw inputs and the resolved profile, not just
// semantic changes or public summaries. It is ephemeral and never logged.
func Observation(c Config, plan PlanResult) string {
	// Item embeds Resource's custom JSON marshaler, so serializing Item directly
	// would silently omit its raw values. Use explicit non-embedded fields.
	type observedItem struct {
		Resource Resource
		Key      string
		Values   map[string]*Snapshot
	}
	items := make([]observedItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, observedItem{item.Resource, item.Key, item.Values})
	}
	data, _ := json.Marshal(struct {
		Config         Config
		Items          []observedItem
		Manifest       Manifest
		ManifestBefore *Snapshot
	}{c, items, plan.Manifest, plan.ManifestBefore})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type Summary struct {
	ID     string   `json:"id"`
	File   string   `json:"file,omitempty"`
	Scope  string   `json:"scope"`
	Kind   string   `json:"kind"`
	Status string   `json:"status"`
	Writes []string `json:"writes"`
	Reason string   `json:"reason,omitempty"`
}

func manifestPath(c Config) string { return filepath.Join(c.StateDir, "manifest.json") }
func pendingPath(c Config) string  { return filepath.Join(c.StateDir, "pending.json") }

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func readManifest(c Config) (Manifest, *Snapshot, error) {
	m := Manifest{2, map[string]Resource{}, map[string]string{}}
	before, err := snapshot(manifestPath(c))
	if err != nil {
		return m, nil, err
	}
	if before == nil {
		return m, nil, nil
	}
	m = Manifest{}
	if err = decode(before, &m); err != nil {
		return m, nil, err
	}
	if m.Version != 2 || m.Resources == nil || m.Files == nil {
		return m, nil, fmt.Errorf("legacy or invalid manifest: preserve existing state and bootstrap into a fresh stateDir")
	}
	for _, digest := range m.Files {
		if !digestPattern.MatchString(digest) {
			return m, nil, fmt.Errorf("invalid manifest digest")
		}
	}
	for _, r := range c.Resources {
		if previous, ok := m.Resources[r.ID]; ok && !reflect.DeepEqual(previous, r) {
			return m, nil, fmt.Errorf("managed resource changed identity: %s; use a new ID and review adoption", r.ID)
		}
	}
	return m, before, nil
}
func expand(r Resource, m Manifest) ([]Item, error) {
	if r.Kind == "instruction-file" || r.Kind == "agent-file" || r.Kind == "hook-config" {
		return []Item{{Resource: r, Key: r.ID, Adapter: r.Kind}}, nil
	}
	if r.Kind == "mcp-config" {
		return []Item{{Resource: r, Key: r.ID, Adapter: "mcp"}}, nil
	}
	if r.Kind == "plugin-directory" {
		return expandPlugin(r, m)
	}
	if r.Kind == "portable-file" {
		return []Item{{Resource: r, Key: r.ID}}, nil
	}
	names := map[string]bool{}
	for _, side := range sides {
		files, err := walk(r.Paths[side])
		if err != nil {
			return nil, err
		}
		hasSkill := false
		for _, file := range files {
			names[file] = true
			if file == "SKILL.md" {
				hasSkill = true
			}
		}
		if len(files) > 0 && !hasSkill {
			return nil, fmt.Errorf("skill directory must contain SKILL.md: %s", r.ID)
		}
	}
	prefix := r.ID + "/"
	for key := range m.Files {
		if strings.HasPrefix(key, prefix) {
			names[strings.TrimPrefix(key, prefix)] = true
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no skill source exists: %s", r.ID)
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	result := []Item{}
	for _, name := range ordered {
		if r.AllowReformat && strings.EqualFold(name, "agents/openai.yaml") {
			return nil, fmt.Errorf("strict skill metadata does not translate host-specific agents/openai.yaml")
		}
		if filepath.IsAbs(name) || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("unsafe skill entry in manifest")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, fmt.Errorf("unsafe skill entry in manifest")
			}
		}
		entry := r
		entry.Paths = map[string]string{}
		for _, side := range sides {
			entry.Paths[side] = filepath.Join(r.Paths[side], name)
		}
		item := Item{Resource: entry, Key: prefix + name, Relative: name}
		if r.AllowReformat && name == "SKILL.md" {
			item.Adapter = "skill-metadata"
		}
		result = append(result, item)
	}
	return result, nil
}
func Plan(c Config) (PlanResult, error) {
	result := PlanResult{Items: []Item{}}
	if err := validateLinks(c); err != nil {
		return result, err
	}
	pending, err := snapshot(pendingPath(c))
	if err != nil {
		return result, err
	}
	if pending != nil {
		return result, fmt.Errorf("an interrupted transaction requires recover before syncing")
	}
	result.Manifest, result.ManifestBefore, err = readManifest(c)
	if err != nil {
		return result, err
	}
	for _, r := range c.Resources {
		entries, err := expand(r, result.Manifest)
		if err != nil {
			return result, err
		}
		for _, item := range entries {
			item.Values = map[string]*Snapshot{}
			semantic := map[string]*Snapshot{}
			hashes := map[string]string{}
			for _, side := range sides {
				value, err := snapshot(item.Paths[side])
				if err != nil {
					return result, err
				}
				item.Values[side] = value
				semantic[side] = value
				switch item.Adapter {
				case "skill-metadata":
					semantic[side], err = normalizeSkill(value)
				case "instruction-file":
					semantic[side], err = normalizeInstructions(side, value)
				case "agent-file":
					semantic[side], err = normalizeAgent(side, value)
				case "hook-config":
					semantic[side], err = normalizeHooks(side, value)
				case "mcp":
					semantic[side], err = normalizeMCP(item.Resource, side, value)
				case "plugin-manifest":
					semantic[side], err = normalizePluginManifest(value)
				}
				if err != nil {
					return result, err
				}
				hashes[side] = fingerprint(semantic[side])
			}
			baseline, tracked := result.Manifest.Files[item.Key]
			selected := ""
			distinct := map[string]bool{}
			for _, side := range sides {
				hash := hashes[side]
				if !tracked {
					if hash != "" {
						distinct[hash] = true
						if selected == "" {
							selected = side
						}
					}
				} else if hash != baseline {
					if hash == "" {
						item.Conflict = "deletion detected; automatic deletion is disabled"
					}
					distinct[hash] = true
					if selected == "" {
						selected = side
					}
				}
			}
			if !tracked {
				if len(distinct) == 0 {
					item.Conflict = "no source exists"
				} else if len(distinct) > 1 {
					item.Conflict = "initial contents or executable bits differ"
				}
			} else if item.Conflict == "" && len(distinct) > 1 {
				item.Conflict = "concurrent edits differ"
			}
			if selected == "" {
				selected = "shared"
			}
			item.Content = semantic[selected]
			if item.Adapter == "mcp" && tracked {
				merged, conflict, handled, err := mergeMCPServers(item.Resource, semantic, result.Manifest)
				if err != nil {
					return result, err
				}
				if handled {
					item.Content = merged
					item.Conflict = conflict
				}
			}
			item.Digest = fingerprint(item.Content)
			item.Writes = []string{}
			if item.Conflict == "" {
				for _, side := range sides {
					if hashes[side] != item.Digest {
						item.Writes = append(item.Writes, side)
					}
				}
			}
			result.Items = append(result.Items, item)
		}
	}
	return result, nil
}
func (p PlanResult) HasConflicts() bool {
	for _, i := range p.Items {
		if i.Conflict != "" {
			return true
		}
	}
	return false
}
func (p PlanResult) Summaries() []Summary {
	result := []Summary{}
	for _, i := range p.Items {
		status := "in-sync"
		if i.Conflict != "" {
			status = "conflict"
		} else if len(i.Writes) > 0 {
			status = "pending"
		}
		result = append(result, Summary{i.ID, i.Relative, i.Scope, i.Kind, status, i.Writes, i.Conflict})
	}
	return result
}
