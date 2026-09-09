package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runPluginCopy(args []string, out, errOut io.Writer) int {
	if len(args) != 5 {
		return usage(errOut)
	}
	report, err := bridge.ComparePluginCopy(args[1], args[2], args[3], args[4])
	if err != nil {
		fmt.Fprintln(errOut, "Plugin copy comparison failed; inspect the selected profile, side and explicit copy root privately.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return 1
	}
	if !report.Matches {
		return 2
	}
	return 0
}
