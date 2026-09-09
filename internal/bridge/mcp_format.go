package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type textEdit struct {
	start, end int
	value      []byte
}

func formatKey(path []string) string {
	b, _ := json.Marshal(path)
	return string(b)
}

// Preserve existing document bytes where only scalar values changed. Structural
// edits use the explicitly consented full renderer. Never trust offsets alone:
// the patched document must decode to exactly the intended complete document.
func preserveMCPText(side string, before *Snapshot, desired map[string]any) *Snapshot {
	if before == nil {
		return nil
	}
	original, err := document(side, before)
	if err != nil {
		return nil
	}
	changes := map[string]any{}
	var diff func([]string, any, any) bool
	diff = func(path []string, old, next any) bool {
		if reflect.DeepEqual(old, next) {
			return true
		}
		a, am := old.(map[string]any)
		b, bm := next.(map[string]any)
		if am || bm {
			if !am || !bm || len(a) != len(b) {
				return false
			}
			for key, value := range a {
				n, ok := b[key]
				if !ok || !diff(append(append([]string{}, path...), key), value, n) {
					return false
				}
			}
			return true
		}
		// Arrays and inline objects are not replaced wholesale: doing so could
		// silently discard comments embedded inside a TOML container.
		switch next.(type) {
		case string, bool, int64, float64, json.Number:
		default:
			return false
		}
		switch old.(type) {
		case string, bool, int64, float64, json.Number:
		default:
			return false
		}
		changes[formatKey(path)] = next
		return true
	}
	// Round-trip the target first to normalize generated []string/maps to the
	// decoder's representation, including TOML numeric types.
	var target []byte
	if side == "codex" {
		target, err = toml.Marshal(desired)
	} else {
		target, err = json.Marshal(desired)
	}
	if err != nil {
		return nil
	}
	normalized, err := document(side, &Snapshot{Data: base64.StdEncoding.EncodeToString(target)})
	if err != nil || !diff(nil, original, normalized) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(before.Data)
	if err != nil {
		return nil
	}
	edits := []textEdit{}
	add := func(path []string, start, end int) {
		key := formatKey(path)
		value, ok := changes[key]
		if !ok {
			return
		}
		var replacement []byte
		var err error
		if side == "codex" {
			replacement, err = toml.Marshal(map[string]any{"v": value})
			replacement = bytes.TrimSpace(bytes.TrimPrefix(replacement, []byte("v = ")))
		} else {
			replacement, err = json.Marshal(value)
		}
		if err == nil && start >= 0 && end > start && end <= len(data) {
			edits = append(edits, textEdit{start, end, replacement})
			delete(changes, key)
		}
	}
	if side == "codex" {
		var p unstable.Parser
		p.Reset(data)
		var table []string
		for p.NextExpression() {
			n := p.Expression()
			if n.Kind == unstable.ArrayTable {
				return nil
			}
			if n.Kind != unstable.Table && n.Kind != unstable.KeyValue {
				continue
			}
			var keys []string
			it := n.Key()
			for it.Next() {
				keys = append(keys, string(it.Node().Data))
			}
			if n.Kind == unstable.Table {
				table = keys
			} else {
				v := n.Value()
				if v.Kind != unstable.Array && v.Kind != unstable.InlineTable {
					add(append(append([]string{}, table...), keys...), int(v.Raw.Offset), int(v.Raw.Offset+v.Raw.Length))
				}
			}
		}
		if p.Error() != nil {
			return nil
		}
	} else {
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		var walk func([]string) bool
		walk = func(path []string) bool {
			start := int(d.InputOffset())
			for start < len(data) && bytes.ContainsRune([]byte(" \t\r\n:,"), rune(data[start])) {
				start++
			}
			token, err := d.Token()
			if err != nil {
				return false
			}
			if delim, ok := token.(json.Delim); ok {
				for d.More() {
					var child []string
					if delim == '{' {
						key, err := d.Token()
						if err != nil {
							return false
						}
						child = append(append([]string{}, path...), key.(string))
					}
					if !walk(child) {
						return false
					}
				}
				_, err = d.Token()
				return err == nil
			}
			add(path, start, int(d.InputOffset()))
			return true
		}
		if !walk(nil) {
			return nil
		}
	}
	if len(changes) != 0 {
		return nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	position := 0
	for _, edit := range edits {
		if edit.start < position {
			return nil
		}
		out.Write(data[position:edit.start])
		out.Write(edit.value)
		position = edit.end
	}
	out.Write(data[position:])
	result := &Snapshot{Data: base64.StdEncoding.EncodeToString(out.Bytes()), Mode: before.Mode}
	actual, err := document(side, result)
	if err != nil || !reflect.DeepEqual(actual, normalized) {
		return nil
	}
	return result
}
