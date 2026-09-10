package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// FileChange deliberately excludes entry points, mixed host configuration and
// entire resources. Empty Rename means delete the selected supporting file.
type FileChange struct {
	Key    string `json:"key"`
	Rename string `json:"rename,omitempty"`
}
type FileChangeReview struct {
	Observation string     `json:"observation"`
	Change      FileChange `json:"change"`
	Copies      int        `json:"copies"`
	Warning     string     `json:"warning"`
}
type fileChangeJournal struct {
	Version    int         `json:"version"`
	Restore    bool        `json:"restore,omitempty"`
	Change     FileChange  `json:"change"`
	Resource   Resource    `json:"resource"`
	Operations []Operation `json:"operations"`
	Created    []bool      `json:"created"`
}

func fileChangePending(c Config) string { return filepath.Join(c.StateDir, "file-change-pending.json") }
func checkFileChangePending(c Config) error {
	s, err := snapshot(fileChangePending(c))
	if err != nil {
		return err
	}
	if s != nil {
		return fmt.Errorf("interrupted supporting-file change requires recover-file-change")
	}
	return nil
}
func supportingPath(kind, path string) bool {
	if filepath.IsAbs(path) || filepath.Clean(path) != path || strings.Contains(path, "\\") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if kind == "plugin-directory" && len(parts) > 3 && parts[0] == "skills" {
		parts = parts[2:]
	}
	return (kind == "skill-directory" || kind == "plugin-directory") && len(parts) >= 2 && (parts[0] == "scripts" || parts[0] == "assets" || parts[0] == "references")
}
func fileChangeResource(c Config, change FileChange) (Resource, string, error) {
	for _, r := range c.Resources {
		prefix := r.ID + "/"
		if !strings.HasPrefix(change.Key, prefix) {
			continue
		}
		relative := strings.TrimPrefix(change.Key, prefix)
		if !supportingPath(r.Kind, relative) || len(r.Links) != 0 || (change.Rename != "" && (!supportingPath(r.Kind, change.Rename) || strings.EqualFold(relative, change.Rename))) {
			return r, "", fmt.Errorf("only unlinked supporting files can be explicitly renamed or deleted")
		}
		return r, relative, nil
	}
	return Resource{}, "", fmt.Errorf("unknown supporting file")
}

func prepareFileChange(c Config, change FileChange) (fileChangeJournal, string, error) {
	var journal fileChangeJournal
	r, relative, err := fileChangeResource(c, change)
	if err != nil {
		return journal, "", err
	}
	p, err := Plan(c)
	if err != nil {
		return journal, "", err
	}
	if p.HasConflicts() || p.ManifestBefore == nil {
		return journal, "", fmt.Errorf("supporting-file changes require an established conflict-free baseline")
	}
	var selected *Item
	for i := range p.Items {
		item := &p.Items[i]
		if len(item.Writes) != 0 {
			return journal, "", fmt.Errorf("synchronize pending edits before a supporting-file change")
		}
		if item.Key == change.Key {
			selected = item
		}
	}
	if selected == nil || selected.Adapter != "" {
		return journal, "", fmt.Errorf("supporting file is not a raw managed item")
	}
	// A move removes the old name just like deletion. Do not leave runtime
	// references dangling; rename undo comes through this same preparation.
	content := selected.Content
	selected.Content = nil
	err = validatePlannedPluginContents(p)
	selected.Content = content
	if err != nil {
		return journal, "", err
	}
	journal = fileChangeJournal{Version: 1, Change: change, Resource: r, Operations: []Operation{}}
	for _, side := range sides {
		before := selected.Values[side]
		if before == nil {
			return journal, "", fmt.Errorf("all supporting-file copies must exist before review")
		}
		if change.Rename != "" {
			dest := filepath.Join(r.Paths[side], change.Rename)
			for _, item := range p.Items {
				for _, path := range item.Paths {
					if strings.EqualFold(path, dest) || inside(strings.ToLower(path), strings.ToLower(dest)) || inside(strings.ToLower(dest), strings.ToLower(path)) {
						return journal, "", fmt.Errorf("rename destination overlaps an existing managed path")
					}
				}
			}
			current, err := snapshot(dest)
			if err != nil || current != nil {
				return journal, "", fmt.Errorf("rename destination must be absent and safe")
			}
			journal.Operations = append(journal.Operations, Operation{"rename-" + side, dest, nil, before})
		}
		journal.Operations = append(journal.Operations, Operation{"remove-" + side, filepath.Join(r.Paths[side], relative), before, nil})
	}
	observation := Observation(c, p)
	delete(p.Manifest.Files, change.Key)
	if change.Rename != "" {
		p.Manifest.Files[r.ID+"/"+change.Rename] = selected.Digest
	}
	after, err := encoded(p.Manifest)
	if err != nil {
		return journal, "", err
	}
	journal.Operations = append(journal.Operations, Operation{"manifest", manifestPath(c), p.ManifestBefore, after})
	journal.Created = make([]bool, len(journal.Operations))
	data, err := json.Marshal(struct {
		Observation string
		Journal     fileChangeJournal
	}{observation, journal})
	if err != nil {
		return journal, "", err
	}
	sum := sha256.Sum256(data)
	return journal, hex.EncodeToString(sum[:]), nil
}

func ReviewFileChange(filename string, change FileChange) (FileChangeReview, error) {
	c, err := LoadConfig(filename)
	if err != nil {
		return FileChangeReview{}, err
	}
	_, observation, err := prepareFileChange(c, change)
	return FileChangeReview{observation, change, 3, "Supporting-file references are not rewritten. Review callers and instructions before applying; backups retain the original copies."}, err
}

func ApplyFileChange(filename, observation string, change FileChange) (Recovery, error) {
	return applyFileChange(filename, observation, change, nil)
}
func applyFileChange(filename, observation string, change FileChange, beforeWrite func(int, Operation) error) (Recovery, error) {
	return applyReviewedFileChange(filename, observation, func(c Config) (fileChangeJournal, string, error) {
		return prepareFileChange(c, change)
	}, beforeWrite)
}

func applyReviewedFileChange(filename, observation string, prepare func(Config) (fileChangeJournal, string, error), beforeWrite func(int, Operation) error) (Recovery, error) {
	result := Recovery{}
	if !digestPattern.MatchString(observation) {
		return result, fmt.Errorf("reviewed observation required")
	}
	c, err := LoadConfig(filename)
	if err != nil {
		return result, err
	}
	err = locked(c, func() error {
		current, err := LoadConfig(filename)
		if err != nil || !reflect.DeepEqual(c, current) {
			return ErrObservationChanged
		}
		journal, digest, err := prepare(c)
		if err != nil {
			return err
		}
		if observation != digest {
			return ErrObservationChanged
		}
		id, err := uuid()
		if err != nil {
			return err
		}
		journalPath := filepath.Join(c.StateDir, "file-change-backups", id, "journal.json")
		if err := writeJSON(journalPath, journal); err != nil {
			return err
		}
		if err := writeJSON(fileChangePending(c), Pending{id}); err != nil {
			return err
		}
		apply := func() error {
			for index, op := range journal.Operations {
				if beforeWrite != nil {
					if err := beforeWrite(index, op); err != nil {
						return err
					}
				}
				current, err := snapshot(op.File)
				if err != nil {
					return err
				}
				if !equal(current, op.Before) {
					return ErrObservationChanged
				}
				if op.Before == nil {
					if err = os.MkdirAll(filepath.Dir(op.File), 0700); err == nil {
						err = createRosterExclusive(op.File, op.After)
					}
					if err == nil {
						journal.Created[index] = true
						err = writeJSON(journalPath, journal)
					}
				} else if op.After == nil {
					err = os.Remove(op.File)
				} else {
					err = writeSnapshot(op.File, op.After)
				}
				if err != nil {
					return err
				}
			}
			return os.Remove(fileChangePending(c))
		}
		if err := apply(); err != nil {
			if rollbackErr := rollbackFileChange(c, journal); rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return fmt.Errorf("supporting-file change rolled back: %w", err)
		}
		result = Recovery{Status: "committed", Transaction: id}
		return nil
	})
	return result, err
}

func validateFileChangeJournal(c Config, j fileChangeJournal) error {
	r, relative, err := fileChangeResource(c, j.Change)
	if err != nil || j.Version != 1 || !reflect.DeepEqual(r, j.Resource) || (j.Restore && j.Change.Rename != "") {
		return fmt.Errorf("file-change journal identity is invalid")
	}
	expected := []string{}
	for _, side := range sides {
		if j.Change.Rename != "" {
			expected = append(expected, filepath.Join(r.Paths[side], j.Change.Rename))
		}
		expected = append(expected, filepath.Join(r.Paths[side], relative))
	}
	expected = append(expected, manifestPath(c))
	if len(j.Operations) != len(expected) || len(j.Created) != len(expected) {
		return fmt.Errorf("invalid file-change operation count")
	}
	manifestOp := j.Operations[len(j.Operations)-1]
	var beforeManifest, afterManifest Manifest
	if decodeEnrollmentJSON(manifestOp.Before, &beforeManifest) != nil || decodeEnrollmentJSON(manifestOp.After, &afterManifest) != nil || beforeManifest.Version != 2 || !reflect.DeepEqual(beforeManifest.Resources[r.ID], r) || beforeManifest.Files == nil {
		return fmt.Errorf("invalid file-change manifest")
	}
	digest, tracked := beforeManifest.Files[j.Change.Key]
	if j.Restore {
		if tracked {
			return fmt.Errorf("restoration baseline already exists")
		}
		digest = afterManifest.Files[j.Change.Key]
		if !digestPattern.MatchString(digest) {
			return fmt.Errorf("restoration baseline missing")
		}
		beforeManifest.Files[j.Change.Key] = digest
	} else {
		if !tracked || !digestPattern.MatchString(digest) {
			return fmt.Errorf("file-change baseline missing")
		}
		delete(beforeManifest.Files, j.Change.Key)
		if j.Change.Rename != "" {
			if _, exists := beforeManifest.Files[r.ID+"/"+j.Change.Rename]; exists {
				return fmt.Errorf("rename baseline already exists")
			}
			beforeManifest.Files[r.ID+"/"+j.Change.Rename] = digest
		}
	}
	if !reflect.DeepEqual(beforeManifest, afterManifest) {
		return fmt.Errorf("file-change journal changes unrelated baseline data")
	}
	for index, op := range j.Operations {
		if op.File != expected[index] || valid(op.Before) != nil || valid(op.After) != nil {
			return fmt.Errorf("invalid file-change operation")
		}
		isRename := j.Change.Rename != "" && index < len(expected)-1 && index%2 == 0
		isRestore := j.Restore && index < len(expected)-1
		if !isRename && !isRestore && j.Created[index] {
			return fmt.Errorf("invalid file-change ownership receipt")
		}
		if index == len(expected)-1 {
			if op.Before == nil || op.After == nil {
				return fmt.Errorf("invalid manifest snapshots")
			}
		} else if isRestore {
			if op.Before != nil || op.After == nil || fingerprint(op.After) != digest {
				return fmt.Errorf("invalid restoration snapshots")
			}
		} else if isRename {
			if op.Before != nil || op.After == nil || !equal(op.After, j.Operations[index+1].Before) {
				return fmt.Errorf("invalid rename snapshots")
			}
		} else if op.Before == nil || op.After != nil {
			return fmt.Errorf("invalid deletion snapshots")
		} else if fingerprint(op.Before) != digest {
			return fmt.Errorf("file-change snapshot differs from its baseline")
		}
	}
	return nil
}

func rollbackFileChange(c Config, j fileChangeJournal) error {
	if err := validateFileChangeJournal(c, j); err != nil {
		return err
	}
	for index, op := range j.Operations {
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if op.Before == nil && !j.Created[index] && current != nil {
			return fmt.Errorf("ambiguous rename creation ownership requires manual inspection")
		}
		if !equal(current, op.Before) && !equal(current, op.After) {
			return fmt.Errorf("file-change recovery blocked by later edit")
		}
	}
	for index := len(j.Operations) - 1; index >= 0; index-- {
		op := j.Operations[index]
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if equal(current, op.Before) {
			continue
		}
		if !equal(current, op.After) {
			return fmt.Errorf("file-change recovery blocked by later edit")
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
	return os.Remove(fileChangePending(c))
}

func RecoverFileChange(filename string) (Recovery, error) {
	result := Recovery{Status: "nothing-to-recover"}
	c, err := LoadConfig(filename)
	if err != nil {
		return result, err
	}
	err = locked(c, func() error {
		pointer, err := snapshot(fileChangePending(c))
		if err != nil || pointer == nil {
			return err
		}
		var pending Pending
		if err := decodeEnrollmentJSON(pointer, &pending); err != nil || !transactionPattern.MatchString(pending.Transaction) {
			return fmt.Errorf("invalid file-change pending pointer")
		}
		stored, err := snapshot(filepath.Join(c.StateDir, "file-change-backups", pending.Transaction, "journal.json"))
		if err != nil || stored == nil {
			return fmt.Errorf("file-change backup missing or unsafe")
		}
		var journal fileChangeJournal
		if err := decodeEnrollmentJSON(stored, &journal); err != nil {
			return err
		}
		if err := rollbackFileChange(c, journal); err != nil {
			return err
		}
		result = Recovery{Status: "recovered", Transaction: pending.Transaction}
		return nil
	})
	return result, err
}
