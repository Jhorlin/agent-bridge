package bridge

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Candidate reports deliberately expose no input paths, manifest values, or
// component names. Matching evidence is not package or runtime equivalence.
type PluginCandidateReport struct {
	Version      int                                   `json:"version"`
	ReadOnly     bool                                  `json:"readOnly"`
	Status       string                                `json:"status"`
	Left         PluginCandidate                       `json:"left"`
	Right        PluginCandidate                       `json:"right"`
	Comparisons  []PluginManifestComparison            `json:"comparisons"`
	Capabilities map[string]PluginCapabilityComparison `json:"capabilities"`
	Warnings     []string                              `json:"warnings"`
}

type PluginCandidate struct {
	Files     int                       `json:"files"`
	Manifests []PluginManifestCandidate `json:"manifests"`
}

type PluginManifestCandidate struct {
	Layout       string            `json:"layout"`
	Declarations map[string]string `json:"declarations"`
}

type PluginManifestComparison struct {
	LeftLayout  string `json:"leftLayout"`
	RightLayout string `json:"rightLayout"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Repository  string `json:"repository"`
	Homepage    string `json:"homepage"`
}

// Counts refer to conventional file paths only, never parsed skill names or
// declared runtime capabilities. A common path can contain different content.
type PluginCapabilityComparison struct {
	Left        int `json:"left"`
	Right       int `json:"right"`
	CommonPaths int `json:"commonPaths"`
	LeftOnly    int `json:"leftOnly"`
	RightOnly   int `json:"rightOnly"`
}

const candidateManifestLimit = 256 << 10
const candidateDepthLimit = 32

var candidateManifests = []struct{ path, layout string }{
	{".claude-plugin/plugin.json", "claude-compatibility"},
	{".codex-plugin/plugin.json", "codex-compatibility"},
	{"plugin.json", "portable-root"},
}

var candidateComponents = []string{"skills", "commands", "agents", "hookConfigs", "mcpConfigs"}
var candidateDeclarations = []string{"skills", "commands", "agents", "hooks", "mcpServers", "apps", "interface", "extensions"}

type inspectedPluginCandidate struct {
	report    PluginCandidate
	manifests []map[string]any
	files     map[string]os.FileInfo
}

// ComparePluginCandidates inspects two explicit package roots without a profile,
// discovery, installation, configuration writes, or opening component bodies.
func ComparePluginCandidates(left, right string) (PluginCandidateReport, error) {
	failure := func() (PluginCandidateReport, error) {
		return PluginCandidateReport{}, fmt.Errorf("plugin candidate inspection failed; inspect explicit roots, metadata limits and filesystem safety privately")
	}
	a, err := inspectPluginCandidate(left)
	if err != nil {
		return failure()
	}
	b, err := inspectPluginCandidate(right)
	if err != nil {
		return failure()
	}
	r := PluginCandidateReport{
		Version: 1, ReadOnly: true, Status: "review-required", Left: a.report, Right: b.report,
		Comparisons: []PluginManifestComparison{}, Capabilities: map[string]PluginCapabilityComparison{},
		Warnings: []string{
			"Matching names or provenance fields do not establish equivalence, authenticity, compatibility, or a preferred replacement.",
			"Counts and overlaps describe conventional paths only. Component bodies, declared custom paths, and runtime behavior were not inspected.",
			"No installation, enablement, authentication, trust, or cache freshness was determined or changed. Review native ownership separately.",
			"This is a point-in-time comparison, not a lock against concurrent filesystem changes. No manifest values or file paths are reported.",
		},
	}
	for i, ma := range a.manifests {
		for j, mb := range b.manifests {
			r.Comparisons = append(r.Comparisons, PluginManifestComparison{
				LeftLayout: a.report.Manifests[i].Layout, RightLayout: b.report.Manifests[j].Layout,
				Name: compareCandidateField(ma, mb, "name"), Version: compareCandidateField(ma, mb, "version"),
				Author: compareCandidateField(ma, mb, "author"), Repository: compareCandidateField(ma, mb, "repository"), Homepage: compareCandidateField(ma, mb, "homepage"),
			})
		}
	}
	for _, component := range candidateComponents {
		x, y := candidateComponentPaths(a.files, component), candidateComponentPaths(b.files, component)
		counts := PluginCapabilityComparison{Left: len(x), Right: len(y)}
		for path := range x {
			if y[path] {
				counts.CommonPaths++
			}
		}
		counts.LeftOnly, counts.RightOnly = counts.Left-counts.CommonPaths, counts.Right-counts.CommonPaths
		r.Capabilities[component] = counts
	}
	return r, nil
}

func compareCandidateField(a, b map[string]any, key string) string {
	x, left := a[key]
	y, right := b[key]
	switch {
	case !left && !right:
		return "both-absent"
	case !left:
		return "left-absent"
	case !right:
		return "right-absent"
	case reflect.DeepEqual(x, y):
		return "match"
	default:
		return "mismatch"
	}
}

func inspectPluginCandidate(root string) (inspectedPluginCandidate, error) {
	r := inspectedPluginCandidate{report: PluginCandidate{Manifests: []PluginManifestCandidate{}}}
	files, err := candidateFileInventory(root)
	if err != nil {
		return r, err
	}
	r.files, r.report.Files = files, len(files)
	contents := map[string][]byte{}
	for _, manifest := range candidateManifests {
		info, exists := files[manifest.path]
		if !exists {
			continue
		}
		if info.Size() > candidateManifestLimit {
			return r, fmt.Errorf("candidate manifest too large")
		}
		data, err := inventoryRead(filepath.Join(root, manifest.path))
		if err != nil || len(data) > candidateManifestLimit || !utf8.Valid(data) {
			return r, fmt.Errorf("unsafe candidate manifest")
		}
		var doc map[string]any
		if err := strictJSON(data, &doc); err != nil || doc == nil {
			return r, fmt.Errorf("invalid candidate manifest")
		}
		if err := validateCandidateIdentity(doc); err != nil {
			return r, err
		}
		if schema, exists := doc["$schema"]; exists && schema != portablePluginSchema {
			return r, fmt.Errorf("unrecognized candidate manifest schema")
		}
		entry := PluginManifestCandidate{Layout: manifest.layout, Declarations: map[string]string{}}
		for _, key := range candidateDeclarations {
			entry.Declarations[key] = "absent"
			if _, exists := doc[key]; exists {
				entry.Declarations[key] = "present"
			}
		}
		r.report.Manifests = append(r.report.Manifests, entry)
		r.manifests = append(r.manifests, doc)
		contents[manifest.path] = data
	}
	if len(r.manifests) == 0 {
		return r, fmt.Errorf("no known candidate manifest")
	}
	// Re-read only the known manifests; unknown bodies remain unopened. The
	// inventory comparison also detects ordinary path/replacement/mode changes.
	for name, before := range contents {
		after, err := inventoryRead(filepath.Join(root, name))
		if err != nil || !bytes.Equal(before, after) {
			return r, fmt.Errorf("candidate changed")
		}
	}
	after, err := candidateFileInventory(root)
	if err != nil || len(files) != len(after) {
		return r, fmt.Errorf("candidate inventory changed")
	}
	for name, before := range files {
		current, exists := after[name]
		if !exists || !os.SameFile(before, current) || before.Mode() != current.Mode() || before.Size() != current.Size() || !before.ModTime().Equal(current.ModTime()) {
			return r, fmt.Errorf("candidate inventory changed")
		}
	}
	return r, nil
}

func validateCandidateIdentity(doc map[string]any) error {
	validText := func(value any) bool {
		s, ok := value.(string)
		return ok && len(s) > 0 && len(s) <= 4096 && !strings.ContainsAny(s, "\x00\r\n")
	}
	name, ok := doc["name"].(string)
	if !ok || len(name) > 128 || !safeID.MatchString(name) {
		return fmt.Errorf("invalid candidate name")
	}
	for _, key := range []string{"version", "repository", "homepage"} {
		if value, exists := doc[key]; exists && !validText(value) {
			return fmt.Errorf("invalid candidate identity field")
		}
	}
	if value, exists := doc["author"]; exists {
		if fields, ok := value.(map[string]any); ok {
			if len(fields) == 0 {
				return fmt.Errorf("invalid candidate author")
			}
			for _, field := range fields {
				if !validText(field) {
					return fmt.Errorf("invalid candidate author")
				}
			}
		} else if !validText(value) {
			return fmt.Errorf("invalid candidate author")
		}
	}
	return nil
}

func candidateReadDir(read func(int) ([]fs.DirEntry, error), remaining int) ([]fs.DirEntry, error) {
	entries := []fs.DirEntry{}
	for {
		// One extra entry distinguishes a full budget from an oversized
		// directory. Never ask the filesystem to materialize all entries.
		size := min(128, remaining-len(entries)+1)
		batch, err := read(size)
		if len(batch) > remaining-len(entries) {
			return nil, fmt.Errorf("excessive candidate inventory")
		}
		entries = append(entries, batch...)
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return nil, fmt.Errorf("incomplete candidate inventory")
		}
	}
}

func candidateFileInventory(root string) (map[string]os.FileInfo, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return nil, fmt.Errorf("invalid candidate root")
	}
	if err := assertSafe(root); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("candidate root must be a directory")
	}
	files := map[string]os.FileInfo{}
	remaining := inventoryEntryLimit - 1 // The root is an inventory entry too.
	var visit func(string) error
	visit = func(directory string) error {
		if err := assertSafe(directory); err != nil {
			return err
		}
		dir, err := openNoFollow(directory)
		if err != nil {
			return err
		}
		entries, readErr := candidateReadDir(dir.ReadDir, remaining)
		closeErr := dir.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := assertSafe(directory); err != nil {
			return err
		}
		remaining -= len(entries)
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe candidate inventory")
			}
			path := filepath.Join(directory, entry.Name())
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if strings.Count(rel, "/") >= candidateDepthLimit {
				return fmt.Errorf("candidate inventory too deep")
			}
			for _, manifest := range candidateManifests {
				if strings.EqualFold(rel, manifest.path) && (rel != manifest.path || entry.IsDir()) {
					return fmt.Errorf("ambiguous candidate manifest path")
				}
			}
			if entry.IsDir() {
				if err := visit(path); err != nil {
					return err
				}
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("unsafe candidate entry")
			}
			files[rel] = info
		}
		return nil
	}
	err = visit(root)
	return files, err
}

func candidateComponentPaths(files map[string]os.FileInfo, component string) map[string]bool {
	paths := map[string]bool{}
	for name := range files {
		parts := strings.Split(name, "/")
		match := false
		switch component {
		case "skills":
			match = len(parts) >= 3 && parts[0] == "skills" && parts[len(parts)-1] == "SKILL.md"
		case "commands":
			match = len(parts) >= 2 && parts[0] == "commands" && strings.HasSuffix(parts[len(parts)-1], ".md")
		case "agents":
			match = len(parts) == 2 && parts[0] == "agents" && (strings.HasSuffix(parts[1], ".md") || strings.HasSuffix(parts[1], ".toml"))
		case "hookConfigs":
			match = name == "hooks/hooks.json"
		case "mcpConfigs":
			match = name == ".mcp.json" || name == "mcp.json"
		}
		if match {
			paths[name] = true
		}
	}
	return paths
}
