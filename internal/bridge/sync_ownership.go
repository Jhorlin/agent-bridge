package bridge

import (
	"fmt"
	"path/filepath"
)

// Keep journal v1 unchanged. New pending pointers require a separate write
// receipt, so missing evidence cannot silently fall back to legacy recovery.
type syncPending struct {
	Transaction       string `json:"transaction"`
	CreationOwnership bool   `json:"creationOwnership,omitempty"`
	WriteOwnership    bool   `json:"writeOwnership,omitempty"`
}

type syncOwnership struct {
	Version int    `json:"version"`
	Journal string `json:"journal"`
	Created []bool `json:"created"`
	Written []bool `json:"written,omitempty"`
}

func syncOwnershipPath(c Config, tx string) string {
	return filepath.Join(c.StateDir, "backups", tx, "creation-ownership.json")
}

func newSyncOwnership(j Journal) (*syncOwnership, error) {
	raw, err := encoded(j)
	if err != nil {
		return nil, err
	}
	return &syncOwnership{Version: 2, Journal: fingerprint(raw), Created: make([]bool, len(j.Operations)), Written: make([]bool, len(j.Operations))}, nil
}

func validateSyncOwnership(j Journal, receipt *syncOwnership) error {
	if receipt == nil {
		return nil
	} // Actual legacy journals have no receipts.
	expected, err := newSyncOwnership(j)
	if err != nil {
		return err
	}
	if (receipt.Version != 1 && receipt.Version != 2) || receipt.Journal != expected.Journal || len(receipt.Created) != len(j.Operations) {
		return fmt.Errorf("invalid sync creation ownership receipt")
	}
	if (receipt.Version == 1 && receipt.Written != nil) || (receipt.Version == 2 && len(receipt.Written) != len(j.Operations)) {
		return fmt.Errorf("invalid sync write ownership receipt")
	}
	for index, created := range receipt.Created {
		if created && j.Operations[index].Before != nil {
			return fmt.Errorf("invalid sync creation ownership flag")
		}
		if receipt.Version == 2 && j.Operations[index].Before == nil && created != receipt.Written[index] {
			return fmt.Errorf("inconsistent sync creation and write ownership flags")
		}
	}
	return nil
}

func readSyncOwnership(c Config, pointer syncPending, j Journal) (*syncOwnership, error) {
	raw, err := snapshot(syncOwnershipPath(c, pointer.Transaction))
	if err != nil {
		return nil, err
	}
	if raw == nil && !pointer.CreationOwnership && !pointer.WriteOwnership {
		return nil, nil
	}
	var receipt syncOwnership
	if err := decodeEnrollmentJSON(raw, &receipt); err != nil {
		return nil, fmt.Errorf("sync creation ownership receipt missing or invalid; preserve pending state for inspection")
	}
	if err := validateSyncOwnership(j, &receipt); err != nil {
		return nil, err
	}
	if pointer.WriteOwnership && receipt.Version != 2 {
		return nil, fmt.Errorf("pending sync requires replacement ownership evidence")
	}
	return &receipt, nil
}

// Matching after-bytes alone do not prove that this transaction wrote a file.
// Version 1 receipts can prove creations, but not replacements. An interrupted
// write without its durable receipt is ambiguous and must retain pending state.
func checkSyncWriteOwnership(receipt *syncOwnership, index int, op Operation, current *Snapshot) error {
	if receipt == nil {
		return nil
	} // Legacy journals have no retroactive evidence.
	if op.Before == nil && current != nil && !receipt.Created[index] {
		return fmt.Errorf("ambiguous sync creation ownership; preserve the file and inspect pending recovery")
	}
	if op.Before != nil && equal(current, op.After) && !equal(current, op.Before) && (receipt.Version < 2 || !receipt.Written[index]) {
		return fmt.Errorf("ambiguous sync replacement ownership; preserve the file and inspect pending recovery")
	}
	return nil
}
