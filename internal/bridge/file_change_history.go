package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileChangeHistoryEntry struct {
	Transaction string     `json:"transaction"`
	Change      FileChange `json:"change"`
	Restore     bool       `json:"restore"`
}

func readFileChangeHistory(c Config, transaction string) (fileChangeJournal, *Snapshot, error) {
	var journal fileChangeJournal
	if !transactionPattern.MatchString(transaction) {
		return journal, nil, fmt.Errorf("invalid file-change history selection")
	}
	raw, err := snapshot(filepath.Join(c.StateDir, "file-change-backups", transaction, "journal.json"))
	if err != nil || raw == nil {
		return journal, nil, fmt.Errorf("file-change history missing or unsafe")
	}
	if err := decodeEnrollmentJSON(raw, &journal); err != nil {
		return journal, nil, err
	}
	if err := validateFileChangeJournal(c, journal); err != nil {
		return journal, nil, err
	}
	return journal, raw, nil
}

// Like History, these are retained attempts, not certified committed events.
func FileChangeHistory(filename string) ([]FileChangeHistoryEntry, error) {
	c, err := LoadConfig(filename)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(c.StateDir, "file-change-backups")
	if err := assertSafe(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	result := []FileChangeHistoryEntry{}
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		j, _, err := readFileChangeHistory(c, entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, FileChangeHistoryEntry{entry.Name(), j.Change, j.Restore})
	}
	return result, nil
}

func prepareFileChangeUndo(c Config, transaction string) (fileChangeJournal, string, error) {
	var result fileChangeJournal
	old, raw, err := readFileChangeHistory(c, transaction)
	if err != nil {
		return result, "", err
	}
	if old.Restore {
		return result, "", fmt.Errorf("restoration journals cannot be undone; review a new explicit deletion")
	}
	p, err := Plan(c)
	if err != nil {
		return result, "", err
	}
	if p.HasConflicts() || p.ManifestBefore == nil {
		return result, "", fmt.Errorf("undo requires an established conflict-free baseline")
	}
	for _, item := range p.Items {
		if len(item.Writes) != 0 {
			return result, "", fmt.Errorf("synchronize pending edits before undo")
		}
	}
	// Pin the selected native state, but do not restore its old whole manifest.
	// New, unrelated work since the selected transaction remains untouched.
	for _, op := range old.Operations[:len(old.Operations)-1] {
		now, err := snapshot(op.File)
		if err != nil || !equal(now, op.After) {
			return result, "", fmt.Errorf("historical file-change targets have changed")
		}
	}
	var currentObservation string
	if old.Change.Rename != "" {
		relative := strings.TrimPrefix(old.Change.Key, old.Resource.ID+"/")
		inverse := FileChange{Key: old.Resource.ID + "/" + old.Change.Rename, Rename: relative}
		result, currentObservation, err = prepareFileChange(c, inverse)
		if err != nil {
			return result, "", err
		}
	} else {
		if _, exists := p.Manifest.Files[old.Change.Key]; exists {
			return result, "", fmt.Errorf("restore baseline already exists")
		}
		for _, op := range old.Operations[:len(old.Operations)-1] {
			for _, item := range p.Items {
				for _, path := range item.Paths {
					if strings.EqualFold(path, op.File) || inside(strings.ToLower(path), strings.ToLower(op.File)) || inside(strings.ToLower(op.File), strings.ToLower(path)) {
						return result, "", fmt.Errorf("restoration overlaps a current managed path")
					}
				}
			}
		}
		currentObservation = Observation(c, p)
		result = fileChangeJournal{Version: 1, Restore: true, Change: old.Change, Resource: old.Resource, Operations: []Operation{}}
		for index, op := range old.Operations[:len(old.Operations)-1] {
			result.Operations = append(result.Operations, Operation{"restore-" + sides[index], op.File, nil, op.Before})
		}
		p.Manifest.Files[old.Change.Key] = fingerprint(old.Operations[0].Before)
		after, err := encoded(p.Manifest)
		if err != nil {
			return result, "", err
		}
		result.Operations = append(result.Operations, Operation{"manifest", manifestPath(c), p.ManifestBefore, after})
		result.Created = make([]bool, len(result.Operations))
	}
	if err := validateFileChangeJournal(c, result); err != nil {
		return result, "", err
	}
	data, err := json.Marshal(struct {
		Observation string
		Transaction string
		Source      *Snapshot
		Journal     fileChangeJournal
	}{currentObservation, transaction, raw, result})
	if err != nil {
		return result, "", err
	}
	sum := sha256.Sum256(data)
	return result, hex.EncodeToString(sum[:]), nil
}

func ReviewFileChangeUndo(filename, transaction string) (FileChangeReview, error) {
	c, err := LoadConfig(filename)
	if err != nil {
		return FileChangeReview{}, err
	}
	j, observation, err := prepareFileChangeUndo(c, transaction)
	return FileChangeReview{observation, j.Change, 3, "Restores historical supporting-file bytes and modes; undoing a rename also removes its unchanged destination. References are not rewritten. Unrelated current baseline entries are retained."}, err
}

func ApplyFileChangeUndo(filename, observation, transaction string) (Recovery, error) {
	return applyFileChangeUndo(filename, observation, transaction, nil)
}

func applyFileChangeUndo(filename, observation, transaction string, beforeWrite func(int, Operation) error) (Recovery, error) {
	return applyReviewedFileChange(filename, observation, func(c Config) (fileChangeJournal, string, error) {
		return prepareFileChangeUndo(c, transaction)
	}, beforeWrite)
}
