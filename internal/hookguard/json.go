package hookguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Reject duplicate keys and excessive nesting before decoding typed hook data.
func uniqueJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	count := 0
	var value func(int) error
	value = func(depth int) error {
		count++
		if depth > 64 || count > 100000 {
			return fmt.Errorf("JSON limit")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		mark, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch mark {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || seen[strings.ToLower(s)] {
					return fmt.Errorf("duplicate or invalid key")
				}
				seen[strings.ToLower(s)] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

// encoding/json deliberately matches struct fields case-insensitively. Hook
// policy output uses exact native field names instead of accepting aliases.
func exactOutputKeys(data []byte) bool {
	var outer map[string]json.RawMessage
	if json.Unmarshal(data, &outer) != nil || len(outer) != 1 {
		return false
	}
	var specific map[string]json.RawMessage
	if json.Unmarshal(outer["hookSpecificOutput"], &specific) != nil || specific == nil {
		return false
	}
	for key := range specific {
		switch key {
		case "hookEventName", "permissionDecision", "permissionDecisionReason", "additionalContext":
		default:
			return false
		}
	}
	return true
}
