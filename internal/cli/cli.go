package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Jhorlin/agent-bridge/internal/bridge"
	"io"
	"time"
)

func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) < 2 || len(args) > 3 {
		return usage(errOut)
	}
	command, filename := args[0], args[1]
	if command != "plan" && command != "sync" && command != "watch" && command != "recover" {
		return usage(errOut)
	}
	if len(args) == 3 && (command != "watch" || args[2] != "--apply") {
		return usage(errOut)
	}
	last := ""
	for {
		if ctx.Err() != nil {
			return 0
		}
		c, err := bridge.LoadConfig(filename)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if command == "recover" {
			result, err := bridge.Recover(c)
			if err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			if err = json.NewEncoder(out).Encode(result); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			return 0
		}
		result, err := bridge.Plan(c)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		data, err := json.MarshalIndent(result.Summaries(), "", "  ")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if string(data) != last {
			if _, err = fmt.Fprintln(out, string(data)); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			last = string(data)
		}
		conflict := result.HasConflicts()
		if !conflict && (command == "sync" || (command == "watch" && len(args) == 3)) {
			if _, err = bridge.Apply(c, bridge.Options{}); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
		if command != "watch" {
			if conflict {
				return 2
			}
			return 0
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
func usage(w io.Writer) int {
	fmt.Fprintln(w, "Usage: agent-bridge <plan|sync|watch|recover> <config.json> [--apply (watch only)]")
	return 1
}
