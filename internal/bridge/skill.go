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

func portableSkillField(key string) bool {
	return key == "name" || key == "description" || key == "license" || key == "compatibility" || key == "metadata"
}

func skillInformation(key string, value *yaml.Node) (any, error) {
	bad := fmt.Errorf("invalid informational skill metadata; expected strings or a string-to-string metadata map")
	if value.Anchor != "" {
		return nil, bad
	}
	if key != "metadata" {
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.ContainsRune(value.Value, '\x00') {
			return nil, bad
		}
		if key == "compatibility" && utf8.RuneCountInString(value.Value) > 500 {
			return nil, bad
		}
		return value.Value, nil
	}
	if value.Kind != yaml.MappingNode || value.Tag != "!!map" {
		return nil, bad
	}
	result := map[string]string{}
	for i := 0; i < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || k.Anchor != "" || k.Value == "" || strings.ContainsRune(k.Value, '\x00') {
			return nil, bad
		}
		if _, ok := result[k.Value]; ok {
			return nil, bad
		}
		text, err := skillInformation("value", v)
		if err != nil {
			return nil, err
		}
		result[k.Value] = text.(string)
	}
	return result, nil
}

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
	fields := map[string]any{}
	nodes := root.Content[0].Content
	for i := 0; i < len(nodes); i += 2 {
		key, value := nodes[i], nodes[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || !portableSkillField(key.Value) || key.Anchor != "" {
			return nil, fmt.Errorf("strict skill supports common informational metadata; host-specific metadata is unsupported")
		}
		if _, ok := fields[key.Value]; ok {
			return nil, fmt.Errorf("duplicate skill metadata field")
		}
		information, err := skillInformation(key.Value, value)
		if err != nil {
			return nil, err
		}
		fields[key.Value] = information
	}
	name, _ := fields["name"].(string)
	description, _ := fields["description"].(string)
	if len(name) > 64 || !skillName.MatchString(name) {
		return nil, fmt.Errorf("strict skill name requires up to 64 lowercase letters, digits and single internal hyphens")
	}
	if strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 || strings.ContainsAny(description, "\r\n\x00") {
		return nil, fmt.Errorf("strict skill description requires 1-1024 characters on one line")
	}
	license, _ := fields["license"].(string)
	compatibility, _ := fields["compatibility"].(string)
	information, _ := fields["metadata"].(map[string]string)
	metadata, err := yaml.Marshal(struct {
		Name          string            `yaml:"name"`
		Description   string            `yaml:"description"`
		License       string            `yaml:"license,omitempty"`
		Compatibility string            `yaml:"compatibility,omitempty"`
		Metadata      map[string]string `yaml:"metadata,omitempty"`
	}{name, description, license, compatibility, information})
	if err != nil {
		return nil, fmt.Errorf("skill rendering failed")
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte("---\n" + string(metadata) + "---\n" + body)), Mode: raw.Mode}, nil
}
