package bridge

import (
	"fmt"
	"path/filepath"
)

// Keep journal v1 unchanged. New pending pointers require a separate creation
// receipt, so a missing receipt cannot silently fall back to legacy recovery.
type syncPending struct {
	Transaction       string `json:"transaction"`
	CreationOwnership bool   `json:"creationOwnership,omitempty"`
}

type syncOwnership struct {
	Version int    `json:"version"`
	Journal string `json:"journal"`
	Created []bool `json:"created"`
}

func syncOwnershipPath(c Config, tx string) string {
	return filepath.Join(c.StateDir, "backups", tx, "creation-ownership.json")
}

func newSyncOwnership(j Journal) (*syncOwnership, error) {
	raw, err := encoded(j)
	if err != nil {
		return nil, err
	}
	return &syncOwnership{Version: 1, Journal: fingerprint(raw), Created: make([]bool, len(j.Operations))}, nil
}

func validateSyncOwnership(j Journal, receipt *syncOwnership) error {
	if receipt == nil {
		return nil
	} // Actual legacy journals have no receipts.
	expected, err := newSyncOwnership(j)
	if err != nil {
		return err
	}
	if receipt.Version != 1 || receipt.Journal != expected.Journal || len(receipt.Created) != len(j.Operations) {
		return fmt.Errorf("invalid sync creation ownership receipt")
	}
	for index, created := range receipt.Created {
		if created && j.Operations[index].Before != nil {
			return fmt.Errorf("invalid sync creation ownership flag")
		}
	}
	return nil
}

func readSyncOwnership(c Config, pointer syncPending, j Journal) (*syncOwnership, error) {
	raw, err := snapshot(syncOwnershipPath(c, pointer.Transaction))
	if err != nil {
		return nil, err
	}
	if raw == nil && !pointer.CreationOwnership {
		return nil, nil
	}
	var receipt syncOwnership
	if err := decodeEnrollmentJSON(raw, &receipt); err != nil {
		return nil, fmt.Errorf("sync creation ownership receipt missing or invalid; preserve pending state for inspection")
	}
	if err := validateSyncOwnership(j, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}
