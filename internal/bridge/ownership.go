package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

type ownershipRoster struct {
	Version  int      `json:"version"`
	Profiles []string `json:"profiles"`
}

// Called only with the common coordinator lock held. An explicit roster opts
// participating profiles into overlap enforcement; no implicit home scanning.
func enforceOwnership(c Config) error {
	raw, err := snapshot(filepath.Join(c.CoordinationDir, "profiles.json"))
	if err != nil {
		return fmt.Errorf("ownership roster is unsafe or unreadable")
	}
	if raw == nil {
		return nil
	}
	data, err := snapshotBytes(raw)
	if err != nil {
		return fmt.Errorf("ownership roster is unreadable")
	}
	var roster ownershipRoster
	if err := strictJSON(data, &roster); err != nil {
		return fmt.Errorf("invalid ownership roster")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&roster); err != nil || roster.Version != 1 || len(roster.Profiles) == 0 {
		return fmt.Errorf("invalid ownership roster")
	}
	seen := map[string]bool{}
	found := false
	for _, file := range roster.Profiles {
		key := strings.ToLower(file)
		if !filepath.IsAbs(file) || filepath.Clean(file) != file || seen[key] {
			return fmt.Errorf("ownership roster requires distinct absolute profile paths")
		}
		seen[key] = true
		other, err := LoadAuditConfig(file)
		if err != nil {
			return fmt.Errorf("ownership roster profile is missing, unsafe or invalid")
		}
		if other.CoordinationDir != c.CoordinationDir {
			return fmt.Errorf("ownership roster profiles must use the same coordinator")
		}
		if len(c.ConfigFiles) > 0 && file == c.ConfigFiles[0] {
			found = true
			if !reflect.DeepEqual(c, other) {
				return fmt.Errorf("ownership profile changed since loading; reload before retrying")
			}
		}
	}
	if !found {
		return fmt.Errorf("profile is not enrolled in ownership roster")
	}
	if len(roster.Profiles) > 1 {
		report, err := CheckOverlaps(roster.Profiles)
		if err != nil {
			return fmt.Errorf("ownership overlap check failed")
		}
		if len(report.Overlaps) > 0 {
			return fmt.Errorf("ownership overlaps block writes; run check-overlap on the roster profiles")
		}
	}
	return nil
}
