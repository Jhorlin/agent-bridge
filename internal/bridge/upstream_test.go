package bridge

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type upstreamFixture struct {
	ID              string   `json:"id"`
	File            string   `json:"file"`
	SHA256          string   `json:"sha256"`
	Repository      string   `json:"repository"`
	Revision        string   `json:"revision"`
	SourcePath      string   `json:"sourcePath"`
	SourceBlob      string   `json:"sourceBlob"`
	License         string   `json:"license"`
	LicenseFile     string   `json:"licenseFile"`
	LicenseSHA256   string   `json:"licenseSHA256"`
	Transformations []string `json:"transformations"`
	Workstreams     []int    `json:"workstreams"`
	Expected        string   `json:"expected"`
	Reason          string   `json:"reason"`
}

func TestUpstreamFixtures(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("testdata", "upstream"))
	must(t, err)
	root, err = filepath.EvalSymlinks(root)
	must(t, err)
	data, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	must(t, err)
	var catalog struct {
		Version  int               `json:"version"`
		Fixtures []upstreamFixture `json:"fixtures"`
	}
	must(t, strictJSON(data, &catalog))
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	must(t, d.Decode(&catalog))
	if catalog.Version != 1 || len(catalog.Fixtures) == 0 {
		t.Fatal("invalid fixture catalog")
	}
	seen := map[string]bool{}
	allowedFiles := map[string]bool{"catalog.json": true, "README.md": true}
	for _, entry := range catalog.Fixtures {
		t.Run(entry.ID, func(t *testing.T) {
			if !safeID.MatchString(entry.ID) || seen[entry.ID] {
				t.Fatal("invalid or duplicate fixture ID")
			}
			seen[entry.ID] = true
			allowedFiles[entry.File] = true
			allowedFiles[entry.LicenseFile] = true
			for _, path := range []string{entry.File, entry.SourcePath, entry.LicenseFile} {
				if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
					t.Fatal("unsafe fixture path")
				}
			}
			if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(entry.Revision) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(entry.SourceBlob) {
				t.Fatal("source must be pinned")
			}
			if entry.Repository != "anthropics/claude-plugins-official" && entry.Repository != "openai/skills" {
				t.Fatal("unreviewed source repository")
			}
			if entry.License != "Apache-2.0" || entry.Reason == "" || len(entry.Transformations) == 0 || len(entry.Workstreams) == 0 {
				t.Fatal("missing review metadata")
			}
			for _, stream := range entry.Workstreams {
				if stream < 1 || stream > 8 {
					t.Fatal("invalid workstream")
				}
			}
			license, err := os.ReadFile(filepath.Join(root, entry.LicenseFile))
			must(t, err)
			if fmt.Sprintf("%x", sha256.Sum256(license)) != entry.LicenseSHA256 {
				t.Fatal("license digest changed")
			}
			if !strings.Contains(string(license), "Apache License") || !strings.Contains(string(license), "TERMS AND CONDITIONS") {
				t.Fatal("license missing")
			}
			raw, err := snapshot(filepath.Join(root, entry.File))
			must(t, err)
			content, err := snapshotBytes(raw)
			must(t, err)
			if fmt.Sprintf("%x", sha256.Sum256(content)) != entry.SHA256 {
				t.Fatal("fixture changed without a reviewed digest update")
			}
			// Git blob hashes verify these three byte-identical fixtures against
			// their pinned upstream objects. This is integrity, not signature verification.
			blob := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
			if fmt.Sprintf("%x", sha1.Sum(blob)) != entry.SourceBlob {
				t.Fatal("fixture no longer matches the pinned upstream blob")
			}
			switch entry.ID {
			case "fakechat-mcp":
				if entry.Expected != "blocked" {
					t.Fatal("support claim needs new acceptance tests")
				}
				f := newFixture(t)
				f.raw.Resources = []resourceInput{{ID: "upstream", Kind: "mcp-config", Scope: "project", Claude: "mcp.json", Codex: "config.toml", Servers: []string{"fakechat"}, AllowReformat: true}}
				f.write("mcp.json", string(content))
				f.load()
				_, err := Apply(f.c, Options{})
				contains(t, err, "interpolation")
				f.expect("mcp.json", string(content))
				f.missing("config.toml")
				f.missing("state/manifest.json")
				f.missing("state/pending.json")
				if !Audit(f.c).Blocked() {
					t.Fatal("audit misreported unsupported MCP")
				}
			case "fakechat-manifest":
				if entry.Expected != "manifest-roundtrip-only" {
					t.Fatal("unexpected support claim")
				}
				common, err := normalizePluginManifest(raw)
				must(t, err)
				for _, side := range sides {
					out, err := renderPluginManifest(Resource{}, side, common)
					must(t, err)
					again, err := normalizePluginManifest(out)
					must(t, err)
					if fingerprint(common) != fingerprint(again) {
						t.Fatal("manifest round-trip lost fields")
					}
				}
			case "cli-creator-sidecar":
				if entry.Expected != "blocked" {
					t.Fatal("sidecar support requires native evidence")
				}
				f := strictSkillFixture(t)
				f.write("claude-skill/agents/openai.yaml", string(content))
				_, err := Apply(f.c, Options{})
				contains(t, err, "agents/openai.yaml")
				f.expect("claude-skill/agents/openai.yaml", string(content))
				f.missing("codex-skill/SKILL.md")
				f.missing("state/manifest.json")
			default:
				t.Fatal("fixture has no behavior test")
			}
		})
	}
	files, err := walk(root)
	must(t, err)
	for _, file := range files {
		if !allowedFiles[file] {
			t.Fatalf("unreviewed fixture file: %s", file)
		}
	}
}
