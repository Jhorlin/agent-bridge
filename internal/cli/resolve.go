package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runResolve(args []string, out, errOut io.Writer) int {
	start := 2
	if args[0] == "resolve-reviewed" {
		start = 3
	}
	if len(args) <= start {
		return usage(errOut)
	}
	choices := map[string]string{}
	for _, arg := range args[start:] {
		n := strings.LastIndex(arg, "=")
		if n <= 0 || n == len(arg)-1 {
			return usage(errOut)
		}
		key, side := arg[:n], arg[n+1:]
		if _, exists := choices[key]; exists {
			return usage(errOut)
		}
		choices[key] = side
	}
	var result any
	var err error
	if start == 2 {
		result, err = bridge.ReviewResolution(args[1], choices)
	} else {
		result, err = bridge.ResolveReviewed(args[1], args[2], choices)
	}
	if err != nil {
		if errors.Is(err, bridge.ErrObservationChanged) {
			fmt.Fprintln(errOut, "Inputs or conflict choices changed; review the resolution again.")
			return 2
		}
		fmt.Fprintln(errOut, "Resolution failed; inspect choices, profile safety, ownership and pending recovery state privately.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(result); err != nil {
		return 1
	}
	return 0
}
