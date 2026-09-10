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
)

type HistoryChoice struct {
	Transaction string `json:"transaction"`
	Key         string `json:"key"`
	Side        string `json:"side"`
	Snapshot    string `json:"snapshot"`
}

type HistoryEntry struct {
	Transaction string `json:"transaction"`
	Operations  int    `json:"operations"`
}

// History lists retained journal identifiers, not a commit log. A journal can
// describe an interrupted or rolled-back attempt. No native contents are emitted.
func History(filename string) ([]HistoryEntry, error) {
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(c.StateDir, "backups")
	if err := assertSafe(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	result := []HistoryEntry{}
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !transactionPattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("unexpected backup entry")
		}
		j, _, err := readHistoryJournal(c, entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, HistoryEntry{entry.Name(), len(j.Operations)})
	}
	return result, nil
}

func readHistoryJournal(c Config, transaction string) (Journal, *Snapshot, error) {
	var j Journal
	archive, err := historyConfig(c)
	if err != nil {
		return j, nil, err
	}
	if !transactionPattern.MatchString(transaction) {
		return j, nil, fmt.Errorf("invalid historical transaction")
	}
	raw, err := snapshot(filepath.Join(c.StateDir, "backups", transaction, "journal.json"))
	if err != nil {
		return j, nil, err
	}
	if raw == nil {
		return j, nil, fmt.Errorf("historical journal missing")
	}
	data, err := snapshotBytes(raw)
	if err != nil {
		return j, nil, err
	}
	if err := strictJSON(data, &j); err != nil {
		return j, nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&j); err != nil {
		return j, nil, err
	}
	if j.Version != 1 || len(j.Operations) == 0 {
		return j, nil, fmt.Errorf("invalid historical journal")
	}
	seen := map[string]bool{}
	for _, op := range j.Operations {
		if !filepath.IsAbs(op.File) || filepath.Clean(op.File) != op.File || seen[op.File] || !allowedTarget(archive, op.File) {
			return j, nil, fmt.Errorf("historical journal has invalid or no-longer-managed targets")
		}
		seen[op.File] = true
		if err := valid(op.Before); err != nil {
			return j, nil, err
		}
		if op.After == nil {
			return j, nil, fmt.Errorf("historical after snapshot missing")
		}
		if err := valid(op.After); err != nil {
			return j, nil, err
		}
	}
	return j, raw, nil
}

// Retained manifest identities authorize reading archived journal metadata only.
// Never use this configuration for planning, applying, or pending recovery.
// A restore still selects an active item and verifies its historical identity.
func historyConfig(c Config) (Config, error) {
	m, _, err := readManifest(c)
	if err != nil {
		return c, err
	}
	archive := c
	archive.Resources = append([]Resource{}, c.Resources...)
	active := map[string]bool{}
	for _, r := range c.Resources {
		active[r.ID] = true
	}
	for id, r := range m.Resources {
		if !active[id] {
			if r.ID != id {
				return c, fmt.Errorf("invalid archived resource identity")
			}
			archive.Resources = append(archive.Resources, r)
		}
	}
	return archive, nil
}

// prepareHistory selects one recorded file version, then uses today's adapter
// and target overlays. It never restores an old manifest or writes a journal's
// arbitrary path. The current expanded item determines every output path.
func prepareHistory(c Config, p *PlanResult, choice HistoryChoice) (string, error) {
	if choice.Side != "claude" && choice.Side != "codex" && choice.Side != "shared" {
		return "", fmt.Errorf("invalid historical side")
	}
	if choice.Snapshot != "before" && choice.Snapshot != "after" {
		return "", fmt.Errorf("select before or after")
	}
	j, raw, err := readHistoryJournal(c, choice.Transaction)
	if err != nil {
		return "", err
	}
	index := -1
	for n, i := range p.Items {
		if i.Key == choice.Key {
			index = n
		} else if i.Conflict != "" {
			return "", fmt.Errorf("other conflicts block historical restore")
		}
	}
	if index < 0 {
		return "", fmt.Errorf("historical item not registered")
	}
	i := &p.Items[index]
	// Require historical resource identity, not merely a coincidentally equal path.
	identityOK := false
	for _, op := range j.Operations {
		if op.File == manifestPath(c) {
			var m Manifest
			if err := decode(op.After, &m); err != nil || m.Version != 2 {
				return "", fmt.Errorf("invalid historical manifest")
			}
			for _, r := range c.Resources {
				if r.ID == i.ID && reflect.DeepEqual(m.Resources[r.ID], r) {
					identityOK = true
				}
			}
		}
	}
	if !identityOK {
		return "", fmt.Errorf("historical resource identity does not match")
	}
	var selected *Snapshot
	found := false
	for _, op := range j.Operations {
		if op.File == i.Paths[choice.Side] && op.Label == i.Key+"-"+choice.Side {
			found = true
			selected = op.After
			if choice.Snapshot == "before" {
				selected = op.Before
			}
		}
	}
	if i.Adapter == "instruction-set" && choice.Side == "claude" {
		selected, err = instructionHistorySnapshot(c, *i, j, choice.Snapshot)
		if err != nil {
			return "", err
		}
		found = selected != nil
	}
	if !found || selected == nil {
		return "", fmt.Errorf("selected historical file version is absent")
	}
	if i.Adapter == "skill-invocation" && choice.Side == "codex" {
		var policy *Snapshot
		foundPolicy := false
		for _, op := range j.Operations {
			if op.File == skillPolicyPath(*i) && op.Label == i.Key+"-codex-policy" {
				foundPolicy = true
				policy = op.After
				if choice.Snapshot == "before" {
					policy = op.Before
				}
			}
		}
		if !foundPolicy {
			return "", fmt.Errorf("historical skill policy companion missing")
		}
		selected, err = packSkillBundle(selected, policy)
		if err != nil {
			return "", err
		}
	}
	old := *i
	old.Values = map[string]*Snapshot{choice.Side: selected}
	content, err := resolutionContent(old, choice.Side)
	if err != nil || content == nil {
		return "", fmt.Errorf("historical content is not supported by current adapter")
	}
	data, _ := json.Marshal(struct {
		Observation string
		Choice      HistoryChoice
		Journal     *Snapshot
	}{Observation(c, *p), choice, raw})
	sum := sha256.Sum256(data)
	i.Content, i.Digest, i.Conflict = content, fingerprint(content), ""
	i.Writes = []string{}
	for _, side := range sides {
		current, err := resolutionContent(*i, side)
		if err != nil {
			return "", err
		}
		if fingerprint(current) != i.Digest {
			i.Writes = append(i.Writes, side)
		}
	}
	if err := validatePlannedNativeSkills(c, *p); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func ReviewHistory(filename string, choice HistoryChoice) (ReviewCheckpoint, error) {
	r := ReviewCheckpoint{Version: 1, ReadOnly: true}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		return r, err
	}
	p, err := Plan(c)
	if err != nil {
		return r, err
	}
	r.Observation, err = prepareHistory(c, &p, choice)
	if err != nil {
		return r, err
	}
	r.Summaries = p.Summaries()
	return r, nil
}

func RestoreReviewed(filename, observation string, choice HistoryChoice) ([]Summary, error) {
	return RestoreReviewedObserved(filename, observation, choice, nil)
}

func RestoreReviewedObserved(filename, observation string, choice HistoryChoice, sink Observer) ([]Summary, error) {
	if !digestPattern.MatchString(observation) {
		return nil, fmt.Errorf("historical restore requires a review digest")
	}
	c, err := LoadAuditConfig(filename)
	if err != nil {
		observe(sink, "config_load", "bridge.plan", "", filename, "", err)
		return nil, err
	}
	return Apply(c, Options{ExpectedObservation: observation, History: &choice, Observe: sink})
}
