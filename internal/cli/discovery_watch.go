package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

// Emit an initial inventory and changed candidate inventories as JSON lines.
// Discover reads names/metadata, never file contents; this does not enroll,
// reconcile, execute or trust anything. Unsafe roots/errors terminate the watch.
func watchDiscovery(ctx context.Context, root, scope string, out, errOut io.Writer) int {
	last := ""
	for {
		if ctx.Err() != nil {
			return 0
		}
		report, err := bridge.Discover(root, scope)
		if err != nil {
			fmt.Fprintln(errOut, "Discovery watch stopped: root unavailable or unsafe. No files changed.")
			return 1
		}
		data, err := json.Marshal(report)
		if err != nil {
			return 1
		}
		current := string(data)
		if current != last {
			if _, err := fmt.Fprintln(out, current); err != nil {
				return 1
			}
			last = current
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0
		case <-timer.C:
		}
	}
}
