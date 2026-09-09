package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runHistory(args []string, out, errOut io.Writer) int {
	var result any
	var err error
	switch args[0] {
	case "history":
		if len(args) != 2 {
			return usage(errOut)
		}
		result, err = bridge.History(args[1])
	case "review-history":
		if len(args) != 6 {
			return usage(errOut)
		}
		result, err = bridge.ReviewHistory(args[1], bridge.HistoryChoice{Transaction: args[2], Key: args[3], Side: args[4], Snapshot: args[5]})
	case "restore-reviewed":
		if len(args) != 7 {
			return usage(errOut)
		}
		result, err = bridge.RestoreReviewed(args[1], args[2], bridge.HistoryChoice{Transaction: args[3], Key: args[4], Side: args[5], Snapshot: args[6]})
	}
	if err != nil {
		if errors.Is(err, bridge.ErrObservationChanged) {
			fmt.Fprintln(errOut, "Inputs or history changed; review the restore again.")
			return 2
		}
		fmt.Fprintln(errOut, "History operation failed; inspect the selection, journal, profile and recovery state privately.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return 1
	}
	return 0
}
