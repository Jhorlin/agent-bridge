package hookguard

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

func unambiguousPath(path string) bool {
	if path == "" || strings.TrimSpace(path) != path || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return false
	}
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == ".." {
			return false
		}
	}
	return true
}

// PatchTargets accepts a deliberately strict subset of the native patch grammar.
// Unknown constructs fail closed. Both ends of a move are policy targets.
func PatchTargets(patch string) ([]string, error) {
	bad := fmt.Errorf("unrecognized or oversized patch")
	if len(patch) > 1<<20 || strings.ContainsAny(patch, "\x00\r") {
		return nil, bad
	}
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	if len(lines) < 3 || lines[0] != "*** Begin Patch" || lines[len(lines)-1] != "*** End Patch" {
		return nil, bad
	}
	var paths []string
	for i := 1; i < len(lines)-1; {
		line := lines[i]
		kind, path := "", ""
		for _, k := range []string{"Add", "Delete", "Update"} {
			prefix := "*** " + k + " File: "
			if strings.HasPrefix(line, prefix) {
				kind, path = k, strings.TrimPrefix(line, prefix)
				break
			}
		}
		if kind == "" || !unambiguousPath(path) {
			return nil, bad
		}
		paths = append(paths, path)
		i++
		if kind == "Update" && i < len(lines)-1 && strings.HasPrefix(lines[i], "*** Move to: ") {
			p := strings.TrimPrefix(lines[i], "*** Move to: ")
			if !unambiguousPath(p) {
				return nil, bad
			}
			paths = append(paths, p)
			i++
		}
		count := 0
		for i < len(lines)-1 {
			line = lines[i]
			if strings.HasPrefix(line, "*** Add File: ") || strings.HasPrefix(line, "*** Delete File: ") || strings.HasPrefix(line, "*** Update File: ") {
				break
			}
			valid := false
			if kind == "Add" {
				valid = strings.HasPrefix(line, "+")
			}
			if kind == "Update" {
				valid = line == "@@" || strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ")
				if line == "*** End of File" {
					i++
					break
				}
			}
			if !valid {
				return nil, bad
			}
			count++
			i++
		}
		if kind == "Add" && count == 0 {
			return nil, bad
		}
		if len(paths) > 256 {
			return nil, bad
		}
	}
	if len(paths) == 0 {
		return nil, bad
	}
	return paths, nil
}
