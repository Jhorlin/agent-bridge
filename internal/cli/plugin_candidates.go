package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runPluginCandidates(args []string, out, errOut io.Writer) int {
	if len(args) != 3 {
		return usage(errOut)
	}
	report, err := bridge.ComparePluginCandidates(args[1], args[2])
	if err != nil {
		fmt.Fprintln(errOut, "Plugin candidate inspection failed; inspect explicit package roots, metadata limits and filesystem safety privately. No files changed.")
		return 1
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		return 1
	}
	return 0
}
