package bridge

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A set preserves the two files Claude loads at the same directory. The raw
// bundle includes modes for observation/recovery; canonical content does not
// transport native file permissions between hosts.
type instructionBundle struct {
	Root      *Snapshot `json:"root"`
	Alternate *Snapshot `json:"alternate"`
}
type instructionSet struct {
	Root      *string `json:"root"`
	Alternate *string `json:"alternate"`
}

const instructionSourceMarker = "<!-- agent-bridge:source:"

// InstructionCompanionPath returns the second Claude file owned by a set, or
// an empty string for other kinds. Callers include it in protected footprints.
func InstructionCompanionPath(r Resource) string {
	if r.Kind != "instruction-set" {
		return ""
	}
	return filepath.Join(filepath.Dir(r.Paths["claude"]), ".claude", "CLAUDE.md")
}

func packInstructionBundle(b instructionBundle) (*Snapshot, error) {
	if b.Root == nil && b.Alternate == nil {
		return nil, nil
	}
	return encoded(b)
}
func unpackInstructionBundle(raw *Snapshot) (instructionBundle, error) {
	var b instructionBundle
	if raw == nil {
		return b, nil
	}
	if err := decodeEnrollmentJSON(raw, &b); err != nil || (b.Root == nil && b.Alternate == nil) || valid(b.Root) != nil || valid(b.Alternate) != nil {
		return b, fmt.Errorf("invalid instruction source bundle")
	}
	return b, nil
}
func validateInstructionSet(s instructionSet) error {
	if s.Root == nil && s.Alternate == nil {
		return fmt.Errorf("instruction set has no sources")
	}
	for _, body := range []*string{s.Root, s.Alternate} {
		if body == nil {
			continue
		}
		if !utf8.ValidString(*body) || strings.Contains(*body, instructionSourceMarker) {
			return fmt.Errorf("invalid instruction source text or reserved source marker")
		}
		if err := checkInstructionImports(*body); err != nil {
			return err
		}
	}
	return nil
}
func decodeInstructionSet(raw *Snapshot) (instructionSet, error) {
	var s instructionSet
	if err := decodeEnrollmentJSON(raw, &s); err != nil {
		return s, fmt.Errorf("invalid shared instruction set")
	}
	return s, validateInstructionSet(s)
}
func instructionText(raw *Snapshot) *string {
	if raw == nil {
		return nil
	}
	data, _ := snapshotBytes(raw) // callers validate snapshots before decoding
	text := string(data)
	return &text
}
func instructionSnapshot(text *string, before *Snapshot) *Snapshot {
	if text == nil {
		return nil
	}
	mode := uint32(0600)
	if before != nil {
		mode = before.Mode
	}
	return &Snapshot{Data: base64.StdEncoding.EncodeToString([]byte(*text)), Mode: mode}
}

func normalizeInstructionSet(side string, raw *Snapshot) (*Snapshot, error) {
	if raw == nil {
		return nil, nil
	}
	var s instructionSet
	switch side {
	case "shared":
		var err error
		s, err = decodeInstructionSet(raw)
		if err != nil {
			return nil, err
		}
	case "claude":
		b, err := unpackInstructionBundle(raw)
		if err != nil {
			return nil, err
		}
		s = instructionSet{instructionText(b.Root), instructionText(b.Alternate)}
	case "codex":
		data, err := snapshotBytes(raw)
		if err != nil {
			return nil, err
		}
		text := string(data)
		for n, name := range []string{"root", "alternate"} {
			start, end := instructionSourceMarker+name+":start -->\n", "\n"+instructionSourceMarker+name+":end -->\n"
			if !strings.HasPrefix(text, start) {
				continue
			}
			text = strings.TrimPrefix(text, start)
			pos := strings.Index(text, end)
			if pos < 0 {
				return nil, fmt.Errorf("instruction source closing marker missing")
			}
			body := text[:pos]
			if n == 0 {
				s.Root = &body
			} else {
				s.Alternate = &body
			}
			text = text[pos+len(end):]
		}
		if text != "" {
			return nil, fmt.Errorf("instruction set has unmarked text or malformed source sections")
		}
	default:
		return nil, fmt.Errorf("invalid instruction set side")
	}
	if err := validateInstructionSet(s); err != nil {
		return nil, err
	}
	return encoded(s)
}

func renderInstructionSet(side string, content, before *Snapshot) (*Snapshot, error) {
	s, err := decodeInstructionSet(content)
	if err != nil {
		return nil, err
	}
	switch side {
	case "shared":
		return encoded(s)
	case "claude":
		old, err := unpackInstructionBundle(before)
		if err != nil {
			return nil, err
		}
		if (old.Root != nil && s.Root == nil) || (old.Alternate != nil && s.Alternate == nil) {
			return nil, fmt.Errorf("instruction source deletion is disabled")
		}
		return packInstructionBundle(instructionBundle{instructionSnapshot(s.Root, old.Root), instructionSnapshot(s.Alternate, old.Alternate)})
	case "codex":
		var out strings.Builder
		for n, body := range []*string{s.Root, s.Alternate} {
			if body == nil {
				continue
			}
			name := []string{"root", "alternate"}[n]
			out.WriteString(instructionSourceMarker + name + ":start -->\n" + *body + "\n" + instructionSourceMarker + name + ":end -->\n")
		}
		text := out.String()
		return instructionSnapshot(&text, before), nil
	default:
		return nil, fmt.Errorf("invalid instruction set side")
	}
}

func instructionBundleOperations(item Item, before, after *Snapshot) ([]Operation, error) {
	old, err := unpackInstructionBundle(before)
	if err != nil {
		return nil, err
	}
	current, err := unpackInstructionBundle(after)
	if err != nil {
		return nil, err
	}
	var operations []Operation
	for n, next := range []*Snapshot{current.Root, current.Alternate} {
		previous := []*Snapshot{old.Root, old.Alternate}[n]
		if next == nil {
			if previous != nil {
				return nil, fmt.Errorf("instruction source deletion is disabled")
			}
			continue
		}
		label, path := item.Key+"-claude", item.Paths["claude"]
		if n == 1 {
			label, path = item.Key+"-claude-alternate", InstructionCompanionPath(item.Resource)
		}
		// Include unchanged companions to make every recorded Claude version whole.
		operations = append(operations, Operation{label, path, previous, next})
	}
	return operations, nil
}

func checkInstructionSourceDeletion(item Item, semantic map[string]*Snapshot, m Manifest) error {
	for _, raw := range semantic {
		if raw == nil {
			continue
		} // ordinary whole-item deletion handling
		s, err := decodeInstructionSet(raw)
		if err != nil {
			return err
		}
		for n, body := range []*string{s.Root, s.Alternate} {
			name := []string{"root", "alternate"}[n]
			if body == nil && m.Files[item.Key+"@source-"+name] != "" {
				return fmt.Errorf("instruction source deletion is disabled; restore the missing source section")
			}
		}
	}
	return nil
}
func recordInstructionSources(item Item, m *Manifest) error {
	s, err := decodeInstructionSet(item.Content)
	if err != nil {
		return err
	}
	for n, body := range []*string{s.Root, s.Alternate} {
		if body != nil {
			m.Files[item.Key+"@source-"+[]string{"root", "alternate"}[n]] = fingerprint(instructionSnapshot(body, nil))
		}
	}
	return nil
}

func instructionHistorySnapshot(c Config, item Item, j Journal, version string) (*Snapshot, error) {
	var m Manifest
	for _, op := range j.Operations {
		if op.File == manifestPath(c) && op.Label == "manifest" {
			if err := decodeEnrollmentJSON(op.After, &m); err != nil {
				return nil, err
			}
		}
	}
	var b instructionBundle
	for n, name := range []string{"root", "alternate"} {
		expected := m.Files[item.Key+"@source-"+name] != ""
		path, label := item.Paths["claude"], item.Key+"-claude"
		if n == 1 {
			path, label = InstructionCompanionPath(item.Resource), item.Key+"-claude-alternate"
		}
		found := false
		for _, op := range j.Operations {
			if op.File != path || op.Label != label {
				continue
			}
			if !expected {
				return nil, fmt.Errorf("historical instruction source layout mismatch")
			}
			found = true
			value := op.After
			if version == "before" {
				value = op.Before
			}
			if n == 0 {
				b.Root = value
			} else {
				b.Alternate = value
			}
		}
		if expected && !found {
			return nil, fmt.Errorf("historical instruction companion missing; select a complete shared or Codex version")
		}
	}
	return packInstructionBundle(b)
}
