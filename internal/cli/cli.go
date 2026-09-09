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
	if command != "plan" && command != "sync" && command != "watch" && command != "recover" && command != "config" && command != "audit" {
		return usage(errOut)
	}
	if len(args) == 3 && !((command == "watch" && args[2] == "--apply") || (command == "audit" && args[2] == "--json")) {
		return usage(errOut)
	}
	last := ""
	for {
		if ctx.Err() != nil {
			return 0
		}
		var c bridge.Config
		var err error
		if command == "audit" {
			c, err = bridge.LoadAuditConfig(filename)
		} else {
			c, err = bridge.LoadConfig(filename)
		}
		if err != nil {
			if command == "audit" {
				fmt.Fprintln(errOut, "Audit could not load the profile: check schema, adapter, consent, inheritance and path safety. Details withheld to protect configuration values.")
				return 1
			}
			fmt.Fprintln(errOut, err)
			return 1
		}
		if command == "audit" {
			report := bridge.Audit(c)
			if len(args) == 3 {
				err = json.NewEncoder(out).Encode(report)
			} else {
				err = writeAudit(out, report)
			}
			if err != nil {
				fmt.Fprintln(errOut, "Could not write audit report.")
				return 1
			}
			if report.Blocked() {
				return 2
			}
			return 0
		}
		if command == "config" {
			if err = json.NewEncoder(out).Encode(c); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			return 0
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
	fmt.Fprintln(w, "Usage: agent-bridge <config|plan|sync|watch|recover|audit> <config.json> [--apply (watch only) | --json (audit only)]")
	return 1
}

func writeAudit(w io.Writer, report bridge.AuditReport) error {
	if _, err := fmt.Fprintln(w, "Compatibility audit (read-only; host behavior not verified)"); err != nil {
		return err
	}
	for _, r := range report.Resources {
		if _, err := fmt.Fprintf(w, "%s [%s, %s, %s]: %s\n", r.ID, r.Kind, r.Scope, r.Direction, r.Status); err != nil {
			return err
		}
		for _, c := range r.Checks {
			if _, err := fmt.Fprintf(w, "  %s: %s; recognized fields: %v; unsupported fields: %v; redacted unknown fields: %d\n", c.Side, c.Status, c.RecognizedFields, c.UnsupportedFields, c.UnknownFields); err != nil {
				return err
			}
		}
		for _, action := range r.Actions {
			if _, err := fmt.Fprintf(w, "  - %s\n", action); err != nil {
				return err
			}
		}
	}
	return nil
}
