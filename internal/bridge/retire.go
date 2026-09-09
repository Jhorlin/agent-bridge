package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

// Retirement changes only the selected profile. Baselines and every resource
// file are deliberately retained, including external plugin-agent exports.
type Retirement struct {
	Resource    string `json:"resource"`
	Observation string `json:"observation"`
	Status      string `json:"status"`
	Backup      string `json:"backup,omitempty"`
}

func checkCurrentConfig(c Config) error {
	if len(c.ConfigFiles) == 0 {
		return nil
	}
	current, err := LoadAuditConfig(c.ConfigFiles[0])
	if err != nil {
		if c.Conventions != nil {
			return ErrObservationChanged
		}
		return err
	}
	if !reflect.DeepEqual(c, current) {
		if c.Conventions != nil {
			return ErrObservationChanged
		}
		return fmt.Errorf("profile changed; reload before writing")
	}
	return nil
}

func prepareRetirement(filename, id string) (Config, *Snapshot, *Snapshot, Retirement, error) {
	r := Retirement{Resource: id, Status: "reviewed-preserve-files"}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return c, nil, nil, r, err
	}
	found := false
	for _, resource := range c.Resources {
		if resource.ID == id {
			found = true
		}
	}
	if !found {
		return c, nil, nil, r, fmt.Errorf("resource is not active")
	}
	// Pending transactions must remain recoverable under their original profile.
	if err = checkFileChangePending(c); err != nil {
		return c, nil, nil, r, err
	}
	pending, err := snapshot(pendingPath(c))
	if err != nil {
		return c, nil, nil, r, err
	}
	if pending != nil {
		return c, nil, nil, r, fmt.Errorf("recover pending sync before retirement")
	}
	files := map[string]*Snapshot{}
	if c.CoordinationDir != "" {
		rosterPath := filepath.Join(c.CoordinationDir, "profiles.json")
		roster, err := snapshot(rosterPath)
		if err != nil {
			return c, nil, nil, r, err
		}
		files[rosterPath] = roster
		if roster != nil {
			var entries ownershipRoster
			if err := decodeEnrollmentJSON(roster, &entries); err != nil || entries.Version != 1 || len(entries.Profiles) == 0 {
				return c, nil, nil, r, fmt.Errorf("invalid ownership roster")
			}
			for _, profile := range entries.Profiles {
				other, err := LoadAuditConfig(profile)
				if err != nil {
					return c, nil, nil, r, err
				}
				for _, file := range other.ConfigFiles {
					if profile != c.ConfigFiles[0] && file == c.ConfigFiles[0] {
						return c, nil, nil, r, fmt.Errorf("retirement blocked: another enrolled profile inherits this profile; retire in the leaf profile or review and remove the dependency first")
					}
					files[file], err = snapshot(file)
					if err != nil {
						return c, nil, nil, r, err
					}
				}
			}
		}
	}
	for _, file := range c.ConfigFiles {
		files[file], err = snapshot(file)
		if err != nil {
			return c, nil, nil, r, err
		}
	}
	before := files[c.ConfigFiles[0]]
	var raw configInput
	if err = decodeEnrollmentJSON(before, &raw); err != nil {
		return c, nil, nil, r, err
	}
	if c.Conventions != nil {
		for _, resource := range c.Resources {
			if resource.ID != id {
				continue
			}
			for _, side := range sides[1:] {
				path := resource.Paths[side]
				if inside(c.Conventions.Root, path) && (automaticResource(resource.ID) || filepath.Base(path) == "CLAUDE.md" || filepath.Base(path) == "AGENTS.md") {
					rel, e := filepath.Rel(c.Conventions.Root, path)
					if e != nil || strings.HasPrefix(rel, "..") {
						return c, nil, nil, r, fmt.Errorf("unsafe convention retirement")
					}
					raw.Conventions.Exclude = append(raw.Conventions.Exclude, rel)
				}
			}
		}
	}
	remaining := []resourceInput{}
	for _, resource := range raw.Resources {
		if resource.ID != id {
			remaining = append(remaining, resource)
		}
	}
	raw.Resources = remaining
	if raw.Extends != "" {
		parent, _, e := inherit(resolve(filepath.Dir(c.ConfigFiles[0]), raw.Extends), map[string]bool{}, true)
		if e != nil {
			return c, nil, nil, r, e
		}
		for _, resource := range parent.Resources {
			if resource.ID == id {
				raw.Disable = append(raw.Disable, id)
				break
			}
		}
	}
	after, err := encoded(raw)
	if err != nil {
		return c, nil, nil, r, err
	}
	after.Mode = before.Mode
	manifest, err := snapshot(manifestPath(c))
	if err != nil {
		return c, nil, nil, r, err
	}
	data, err := json.Marshal(struct {
		Config          Config
		Files           map[string]*Snapshot
		After, Manifest *Snapshot
	}{c, files, after, manifest})
	if err != nil {
		return c, nil, nil, r, err
	}
	hash := sha256.Sum256(data)
	r.Observation = hex.EncodeToString(hash[:])
	return c, before, after, r, nil
}

func ReviewRetirement(filename, id string) (Retirement, error) {
	_, _, _, r, err := prepareRetirement(filename, id)
	return r, err
}

func ApplyRetirement(filename, observation, id string) (Retirement, error) {
	return applyRetirement(filename, observation, id, nil)
}

func applyRetirement(filename, observation, id string, beforeWrite func() error) (Retirement, error) {
	c, err := LoadAuditConfig(filename)
	var result Retirement
	if err != nil {
		return result, err
	}
	err = locked(c, func() error {
		_, before, after, r, err := prepareRetirement(filename, id)
		if err != nil {
			return err
		}
		if !digestPattern.MatchString(observation) || observation != r.Observation {
			return ErrObservationChanged
		}
		tx, err := uuid()
		if err != nil {
			return err
		}
		r.Backup = filepath.Join(c.StateDir, "retirement-backups", tx, "profile.json")
		// Exact private backup first; no multi-file transaction or manifest edit.
		backup := *before
		backup.Mode = 0600
		if err = writeSnapshot(r.Backup, &backup); err != nil {
			return err
		}
		if beforeWrite != nil {
			if err = beforeWrite(); err != nil {
				return err
			}
		}
		_, _, _, latest, err := prepareRetirement(filename, id)
		if err != nil {
			return err
		}
		if latest.Observation != observation {
			return ErrObservationChanged
		}
		if err = writeSnapshot(c.ConfigFiles[0], after); err != nil {
			return err
		}
		r.Status = "retired-files-preserved"
		result = r
		return nil
	})
	return result, err
}
