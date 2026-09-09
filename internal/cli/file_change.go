package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runFileChange(args []string, out, errOut io.Writer) int {
	var result any
	var err error
	if args[0] == "recover-file-change" {
		if len(args) != 2 {
			return usage(errOut)
		}
		result, err = bridge.RecoverFileChange(args[1])
	} else {
		start := 2
		if args[0] == "apply-file-change" {
			start = 3
		}
		if len(args) != start+2 && len(args) != start+3 {
			return usage(errOut)
		}
		choice := bridge.FileChange{Key: args[start]}
		if args[start+1] == "--rename" && len(args) == start+3 {
			choice.Rename = args[start+2]
			if choice.Rename == "" {
				return usage(errOut)
			}
		} else if args[start+1] != "--delete" || len(args) != start+2 {
			return usage(errOut)
		}
		if start == 2 {
			result, err = bridge.ReviewFileChange(args[1], choice)
		} else {
			result, err = bridge.ApplyFileChange(args[1], args[2], choice)
		}
	}
	if err != nil {
		if errors.Is(err, bridge.ErrObservationChanged) {
			fmt.Fprintln(errOut, "Reviewed inputs changed; review the supporting-file change again.")
			return 2
		}
		fmt.Fprintln(errOut, "Supporting-file change failed; inspect baseline, paths, choices, ownership and pending recovery privately.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return 1
	}
	return 0
}
