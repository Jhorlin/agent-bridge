package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnrollReviewed adds an already prepared private profile to its explicit
// coordinator roster. It never creates a profile or changes native resources.
// The only commit is one atomic roster replacement, preceded by a private backup.
func EnrollReviewed(filename, observation string) error {
	if !digestPattern.MatchString(observation) {
		return fmt.Errorf("review observation required")
	}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return fmt.Errorf("invalid enrollment profile")
	}
	if c.CoordinationDir == "" {
		return fmt.Errorf("enrollment requires an explicit coordinationDir")
	}
	return directoryLocked(c.CoordinationDir, func() error {
		if err := checkEnrollmentPending(c.CoordinationDir); err != nil {
			return err
		}
		profile := c.ConfigFiles[0]
		review, err := ReviewProfile(profile)
		if err != nil {
			return err
		}
		if review.Observation != observation {
			return ErrObservationChanged
		}
		path := filepath.Join(c.CoordinationDir, "profiles.json")
		before, err := snapshot(path)
		if err != nil {
			return err
		}
		roster := ownershipRoster{Version: 1, Profiles: []string{}}
		if before != nil {
			data, err := snapshotBytes(before)
			if err != nil {
				return err
			}
			if err := strictJSON(data, &roster); err != nil {
				return fmt.Errorf("invalid roster")
			}
			d := json.NewDecoder(bytes.NewReader(data))
			d.DisallowUnknownFields()
			if err := d.Decode(&roster); err != nil || roster.Version != 1 || len(roster.Profiles) == 0 {
				return fmt.Errorf("invalid roster")
			}
		}
		seen := map[string]bool{}
		already := false
		for _, p := range roster.Profiles {
			key := strings.ToLower(p)
			if !filepath.IsAbs(p) || filepath.Clean(p) != p || seen[key] {
				return fmt.Errorf("invalid or duplicate roster path")
			}
			seen[key] = true
			if p == profile {
				already = true
			} else if strings.EqualFold(p, profile) {
				return fmt.Errorf("case-colliding profile path")
			}
		}
		if !already {
			roster.Profiles = append(roster.Profiles, profile)
		}
		observed := map[string]*Snapshot{}
		for _, p := range roster.Profiles {
			other, err := LoadAuditConfig(p)
			if err != nil {
				return fmt.Errorf("roster profile invalid or missing")
			}
			if other.CoordinationDir != c.CoordinationDir {
				return fmt.Errorf("roster profiles require the same coordinator")
			}
			for _, file := range other.ConfigFiles {
				raw, err := snapshot(file)
				if err != nil {
					return err
				}
				observed[file] = raw
			}
		}
		if len(roster.Profiles) > 1 {
			report, err := CheckOverlaps(roster.Profiles)
			if err != nil {
				return err
			}
			if len(report.Overlaps) > 0 {
				return fmt.Errorf("ownership overlaps block enrollment")
			}
		}
		// Recheck effective managed inputs and all reviewed profile bytes just
		// before the single-file commit. External writers must still cooperate.
		review, err = ReviewProfile(profile)
		if err != nil {
			return err
		}
		if review.Observation != observation {
			return ErrObservationChanged
		}
		for file, raw := range observed {
			now, err := snapshot(file)
			if err != nil {
				return err
			}
			if !equal(now, raw) {
				return ErrObservationChanged
			}
		}
		current, err := snapshot(path)
		if err != nil {
			return err
		}
		if !equal(current, before) {
			return fmt.Errorf("roster changed during enrollment")
		}
		if already {
			return nil
		}
		after, err := encoded(roster)
		if err != nil {
			return err
		}
		id, err := uuid()
		if err != nil {
			return err
		}
		backup := struct {
			Version int       `json:"version"`
			Before  *Snapshot `json:"before"`
			After   *Snapshot `json:"after"`
		}{1, before, after}
		if err := writeJSON(filepath.Join(c.CoordinationDir, "enrollment-backups", id, "roster.json"), backup); err != nil {
			return err
		}
		current, err = snapshot(path)
		if err != nil {
			return err
		}
		if !equal(current, before) {
			return fmt.Errorf("roster changed before enrollment commit")
		}
		if before == nil {
			return createRosterExclusive(path, after)
		}
		return writeSnapshot(path, after)
	})
}

// Link a fully written private temporary inode into the absent roster name.
// Unlike writing through O_EXCL directly, readers never observe a partial JSON
// document. Link fails if another writer created the destination in the meantime.
func createRosterExclusive(path string, after *Snapshot) error {
	data, err := snapshotBytes(after)
	if err != nil {
		return err
	}
	if err := assertSafe(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".enrollment-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := assertSafe(path); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}
