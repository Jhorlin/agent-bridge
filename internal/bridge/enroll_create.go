package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type enrollmentCreation struct {
	Version        int       `json:"version"`
	Target         string    `json:"target"`
	Profile        *Snapshot `json:"profile"`
	ProfileCreated bool      `json:"profileCreated"`
	RosterBefore   *Snapshot `json:"rosterBefore"`
	RosterAfter    *Snapshot `json:"rosterAfter"`
}

type enrollmentProposal struct {
	Config      Config
	Journal     enrollmentCreation
	Files       map[string]*Snapshot
	Plan        PlanResult
	Observation string
}

func enrollmentPendingPath(dir string) string { return filepath.Join(dir, "enrollment-pending.json") }
func checkEnrollmentPending(dir string) error {
	raw, err := snapshot(enrollmentPendingPath(dir))
	if err != nil {
		return err
	}
	if raw != nil {
		return fmt.Errorf("interrupted profile enrollment requires recover-enrollment")
	}
	return nil
}

func decodeEnrollmentJSON(raw *Snapshot, v any) error {
	data, err := snapshotBytes(raw)
	if err != nil {
		return err
	}
	if err := strictJSON(data, v); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func flatProfile(c Config) (*Snapshot, error) {
	raw := configInput{Version: 1, StateDir: c.StateDir, CoordinationDir: c.CoordinationDir, Resources: []resourceInput{}}
	for _, r := range c.Resources {
		entry := resourceInput{ID: r.ID, Kind: r.Kind, Scope: r.Scope, Portable: true, Claude: r.Paths["claude"], Codex: r.Paths["codex"], Servers: r.Servers, AllowReformat: r.AllowReformat, CodexPluginLayout: r.CodexPluginLayout, PreserveCodexMCPPolicies: r.PreserveCodexMCPPolicies, PreserveAgentSettings: r.PreserveAgentSettings}
		entry.TranslateSkillInvocation = r.TranslateSkillInvocation
		entry.CodexAgentExports = r.CodexAgentExports
		for side, link := range r.Links {
			if entry.LinkTargets == nil {
				entry.LinkTargets = map[string]string{}
			}
			entry.LinkTargets[side] = link.Target
			if side == "claude" {
				entry.Claude = link.Path
			} else {
				entry.Codex = link.Path
			}
		}
		raw.Resources = append(raw.Resources, entry)
	}
	return encoded(raw)
}

// The template is an already reviewed runnable profile, not the non-runnable
// draft envelope. It is flattened with absolute paths; no source file is moved.
func prepareEnrollmentCreation(template, target string) (enrollmentProposal, error) {
	p := enrollmentProposal{Files: map[string]*Snapshot{}}
	c, err := LoadAuditConfig(template)
	if err != nil {
		return p, err
	}
	if c.CoordinationDir == "" {
		return p, fmt.Errorf("new enrollment requires coordinationDir")
	}
	if err := checkEnrollmentPending(c.CoordinationDir); err != nil {
		return p, err
	}
	if !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return p, fmt.Errorf("new profile target must be clean and absolute")
	}
	if err := assertSafe(target); err != nil {
		return p, err
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		return p, err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), filepath.Base(target)) {
			return p, fmt.Errorf("new profile target already exists or case-collides")
		}
	}
	for _, claim := range configPathClaims(c) {
		x, y := strings.ToLower(target), strings.ToLower(claim.Path)
		if inside(x, y) || inside(y, x) {
			return p, fmt.Errorf("new profile target overlaps template paths")
		}
	}
	for _, file := range c.ConfigFiles {
		raw, err := snapshot(file)
		if err != nil {
			return p, err
		}
		p.Files[file] = raw
	}
	c.ConfigFiles = []string{target}
	p.Config = c
	p.Journal = enrollmentCreation{Version: 1, Target: target}
	p.Journal.Profile, err = flatProfile(c)
	if err != nil {
		return p, err
	}
	rosterPath := filepath.Join(c.CoordinationDir, "profiles.json")
	p.Journal.RosterBefore, err = snapshot(rosterPath)
	if err != nil {
		return p, err
	}
	roster := ownershipRoster{Version: 1, Profiles: []string{}}
	if p.Journal.RosterBefore != nil {
		if err := decodeEnrollmentJSON(p.Journal.RosterBefore, &roster); err != nil || roster.Version != 1 || len(roster.Profiles) == 0 {
			return p, fmt.Errorf("invalid enrollment roster")
		}
	}
	seen := map[string]bool{}
	groups := [][]PathClaim{configPathClaims(c)}
	for _, file := range roster.Profiles {
		key := strings.ToLower(file)
		if !filepath.IsAbs(file) || filepath.Clean(file) != file || seen[key] || strings.EqualFold(file, target) {
			return p, fmt.Errorf("invalid or duplicate roster member")
		}
		seen[key] = true
		other, err := LoadAuditConfig(file)
		if err != nil {
			return p, err
		}
		if other.CoordinationDir != c.CoordinationDir {
			return p, fmt.Errorf("roster coordinator mismatch")
		}
		claims := configPathClaims(other)
		for _, group := range groups {
			if len(overlappingClaims(claims, group)) > 0 {
				return p, fmt.Errorf("ownership overlap blocks profile creation")
			}
		}
		groups = append(groups, claims)
		for _, source := range other.ConfigFiles {
			raw, err := snapshot(source)
			if err != nil {
				return p, err
			}
			p.Files[source] = raw
		}
	}
	roster.Profiles = append(roster.Profiles, target)
	p.Journal.RosterAfter, err = encoded(roster)
	if err != nil {
		return p, err
	}
	p.Plan, err = Plan(c)
	if err != nil {
		return p, err
	}
	if p.Plan.HasConflicts() {
		return p, fmt.Errorf("conflicts block profile creation")
	}
	data, _ := json.Marshal(struct {
		Inputs  string
		Journal enrollmentCreation
		Files   map[string]*Snapshot
	}{Observation(c, p.Plan), p.Journal, p.Files})
	sum := sha256.Sum256(data)
	p.Observation = hex.EncodeToString(sum[:])
	return p, nil
}

func ReviewEnrollmentCreation(template, target string) (ReviewCheckpoint, error) {
	p, err := prepareEnrollmentCreation(template, target)
	if err != nil {
		return ReviewCheckpoint{}, err
	}
	return ReviewCheckpoint{Version: 1, ReadOnly: true, Observation: p.Observation, Summaries: p.Plan.Summaries()}, nil
}

func CreateEnrolledReviewed(template, target, observation string) error {
	return createEnrolledReviewed(template, target, observation, nil)
}

func createEnrolledReviewed(template, target, observation string, beforeWrite func(string) error) error {
	if !digestPattern.MatchString(observation) {
		return fmt.Errorf("review digest required")
	}
	c, err := LoadAuditConfig(template)
	if err != nil {
		return err
	}
	if c.CoordinationDir == "" {
		return fmt.Errorf("coordinationDir required")
	}
	return directoryLocked(c.CoordinationDir, func() error {
		p, err := prepareEnrollmentCreation(template, target)
		if err != nil {
			return err
		}
		if p.Observation != observation || p.Config.CoordinationDir != c.CoordinationDir {
			return ErrObservationChanged
		}
		id, err := uuid()
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(c.CoordinationDir, "enrollment-backups", id, "create.json"), p.Journal); err != nil {
			return err
		}
		if err := writeJSON(enrollmentPendingPath(c.CoordinationDir), Pending{id}); err != nil {
			return err
		}
		writeErr := func() error {
			if beforeWrite != nil {
				if err := beforeWrite("profile"); err != nil {
					return err
				}
			}
			if err := createRosterExclusive(target, p.Journal.Profile); err != nil {
				return err
			}
			p.Journal.ProfileCreated = true
			if err := writeJSON(filepath.Join(c.CoordinationDir, "enrollment-backups", id, "create.json"), p.Journal); err != nil {
				return err
			}
			loaded, err := LoadAuditConfig(target)
			if err != nil || !reflect.DeepEqual(loaded, p.Config) {
				return fmt.Errorf("created profile does not match reviewed configuration")
			}
			if beforeWrite != nil {
				if err := beforeWrite("roster"); err != nil {
					return err
				}
			}
			for file, raw := range p.Files {
				current, err := snapshot(file)
				if err != nil {
					return err
				}
				if !equal(current, raw) {
					return ErrObservationChanged
				}
			}
			plan, err := Plan(p.Config)
			if err != nil {
				return err
			}
			if Observation(p.Config, plan) != Observation(p.Config, p.Plan) {
				return ErrObservationChanged
			}
			current, err := snapshot(target)
			if err != nil {
				return err
			}
			if !equal(current, p.Journal.Profile) {
				return ErrObservationChanged
			}
			rosterPath := filepath.Join(c.CoordinationDir, "profiles.json")
			current, err = snapshot(rosterPath)
			if err != nil {
				return err
			}
			if !equal(current, p.Journal.RosterBefore) {
				return ErrObservationChanged
			}
			if p.Journal.RosterBefore == nil {
				return createRosterExclusive(rosterPath, p.Journal.RosterAfter)
			}
			return writeSnapshot(rosterPath, p.Journal.RosterAfter)
		}()
		if writeErr != nil {
			if !p.Journal.ProfileCreated {
				if err := os.Remove(enrollmentPendingPath(c.CoordinationDir)); err != nil {
					return fmt.Errorf("%w; pending marker requires inspection", writeErr)
				}
				return fmt.Errorf("%w; profile creation was not applied", writeErr)
			}
			if err := rollbackEnrollmentCreation(c.CoordinationDir, p.Journal); err != nil {
				return fmt.Errorf("%w; enrollment pending recovery: %v", writeErr, err)
			}
			return fmt.Errorf("%w; new profile enrollment rolled back", writeErr)
		}
		return os.Remove(enrollmentPendingPath(c.CoordinationDir))
	})
}

func rollbackEnrollmentCreation(dir string, j enrollmentCreation) error {
	if j.Version != 1 || j.Profile == nil || j.RosterAfter == nil || !filepath.IsAbs(j.Target) || filepath.Clean(j.Target) != j.Target || inside(dir, j.Target) || inside(j.Target, dir) {
		return fmt.Errorf("invalid enrollment journal")
	}
	for _, raw := range []*Snapshot{j.Profile, j.RosterBefore, j.RosterAfter} {
		if err := valid(raw); err != nil {
			return err
		}
	}
	rosterPath := filepath.Join(dir, "profiles.json")
	if !j.ProfileCreated {
		current, err := snapshot(j.Target)
		if err != nil {
			return err
		}
		roster, err := snapshot(rosterPath)
		if err != nil {
			return err
		}
		if current != nil || !equal(roster, j.RosterBefore) {
			return fmt.Errorf("profile creation ownership is ambiguous; inspect before recovery")
		}
		return os.Remove(enrollmentPendingPath(dir))
	}
	for _, op := range []Operation{{File: j.Target, After: j.Profile}, {File: rosterPath, Before: j.RosterBefore, After: j.RosterAfter}} {
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if !equal(current, op.Before) && !equal(current, op.After) {
			return fmt.Errorf("later edit blocks enrollment rollback")
		}
	}
	for _, op := range []Operation{{File: rosterPath, Before: j.RosterBefore, After: j.RosterAfter}, {File: j.Target, After: j.Profile}} {
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if equal(current, op.Before) {
			continue
		}
		if !equal(current, op.After) {
			return fmt.Errorf("later edit blocks enrollment rollback")
		}
		if op.Before == nil {
			err = os.Remove(op.File)
		} else {
			err = writeSnapshot(op.File, op.Before)
		}
		if err != nil {
			return err
		}
	}
	return os.Remove(enrollmentPendingPath(dir))
}

func RecoverEnrollmentCreation(target, coordinationDir string) error {
	if !filepath.IsAbs(target) || filepath.Clean(target) != target || !filepath.IsAbs(coordinationDir) || filepath.Clean(coordinationDir) != coordinationDir {
		return fmt.Errorf("explicit absolute target and coordinator required")
	}
	return directoryLocked(coordinationDir, func() error {
		raw, err := snapshot(enrollmentPendingPath(coordinationDir))
		if err != nil || raw == nil {
			return err
		}
		var pointer Pending
		if err := decodeEnrollmentJSON(raw, &pointer); err != nil || !transactionPattern.MatchString(pointer.Transaction) {
			return fmt.Errorf("invalid enrollment recovery pointer")
		}
		raw, err = snapshot(filepath.Join(coordinationDir, "enrollment-backups", pointer.Transaction, "create.json"))
		if err != nil || raw == nil {
			return fmt.Errorf("enrollment journal unavailable")
		}
		var j enrollmentCreation
		if err := decodeEnrollmentJSON(raw, &j); err != nil || j.Target != target {
			return fmt.Errorf("enrollment journal target mismatch")
		}
		return rollbackEnrollmentCreation(coordinationDir, j)
	})
}
