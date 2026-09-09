package bridge

import (
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Strict mode is deliberately opt-in. All peers, including the common store,
// remain usable SKILL.md files. Instruction bytes are not rewritten or executed.
func normalizeSkill(raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := snapshotBytes(raw)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("skill must be valid UTF-8")
	}
	text := string(data)
	first := strings.IndexByte(text, '\n')
	if first < 0 || strings.TrimSuffix(text[:first], "\r") != "---" {
		return nil, fmt.Errorf("strict skill requires YAML frontmatter")
	}
	front, body := "", ""
	found := false
	for start := first + 1; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			break
		}
		end += start
		if strings.TrimSuffix(text[start:end], "\r") == "---" {
			front, body, found = text[first+1:start], text[end+1:], true
			break
		}
		start = end + 1
	}
	if !found || strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("skill requires terminated frontmatter and instructions")
	}
	d := yaml.NewDecoder(strings.NewReader(front))
	var root yaml.Node
	if d.Decode(&root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode || root.Content[0].Tag != "!!map" || root.Content[0].Anchor != "" {
		return nil, fmt.Errorf("invalid skill frontmatter")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("multiple skill frontmatter documents are unsupported")
	}
	fields := map[string]string{}
	nodes := root.Content[0].Content
	for i := 0; i < len(nodes); i += 2 {
		key, value := nodes[i], nodes[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || (key.Value != "name" && key.Value != "description") || value.Kind != yaml.ScalarNode || value.Tag != "!!str" || key.Anchor != "" || value.Anchor != "" {
			return nil, fmt.Errorf("strict skill supports only string name and description; host-specific metadata is unsupported")
		}
		if _, ok := fields[key.Value]; ok {
			return nil, fmt.Errorf("duplicate skill metadata field")
		}
		fields[key.Value] = value.Value
	}
	name, description := fields["name"], fields["description"]
	if len(name) > 64 || !skillName.MatchString(name) {
		return nil, fmt.Errorf("strict skill name requires up to 64 lowercase letters, digits and single internal hyphens")
	}
	if strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 || strings.ContainsAny(description, "\r\n\x00") {
		return nil, fmt.Errorf("strict skill description requires 1-1024 characters on one line")
	}
	metadata, err := yaml.Marshal(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}{name, description})
	if err != nil {
		return nil, fmt.Errorf("skill rendering failed")
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte("---\n" + string(metadata) + "---\n" + body)), Mode: raw.Mode}, nil
}
