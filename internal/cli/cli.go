package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Jhorlin/agent-bridge/internal/bridge"
	"io"
	"time"
)

// Version is set by release builds; ordinary source builds identify as dev.
var Version = "dev"

func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if ctx.Err() != nil {
		return 0
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		if _, err := fmt.Fprintln(out, "agent-bridge "+Version); err != nil {
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "service" {
		return runService(ctx, args[1:], out, errOut)
	}
	if len(args) > 0 && args[0] == "check-overlap" {
		report, err := bridge.CheckOverlaps(args[1:])
		if err != nil {
			fmt.Fprintln(errOut, "Overlap check failed: supply at least two distinct, valid, safe profiles. Configuration details withheld.")
			return 1
		}
		if err := json.NewEncoder(out).Encode(report); err != nil {
			return 1
		}
		if len(report.Overlaps) > 0 {
			return 2
		}
		return 0
	}
	if len(args) < 2 || len(args) > 3 {
		return usage(errOut)
	}
	command, filename := args[0], args[1]
	if command == "init" {
		if len(args) != 2 {
			return usage(errOut)
		}
		if err := bridge.InitProfile(filename); err != nil {
			fmt.Fprintln(errOut, "Could not create profile; check path safety, permissions, and whether it already exists.")
			return 1
		}
		if _, err := fmt.Fprintln(out, "Created an empty private profile. Add reviewed resources, then audit and plan before syncing."); err != nil {
			return 1
		}
		return 0
	}
	if command == "discover" || command == "watch-discovery" {
		if len(args) != 3 || (args[2] != "--global" && args[2] != "--project") {
			return usage(errOut)
		}
		scope := "project"
		if args[2] == "--global" {
			scope = "global"
		}
		if command == "watch-discovery" {
			return watchDiscovery(ctx, filename, scope, out, errOut)
		}
		report, err := bridge.Discover(filename, scope)
		if err != nil {
			fmt.Fprintln(errOut, "Discovery failed: check root, permissions and unsafe links. No files changed.")
			return 1
		}
		if err := json.NewEncoder(out).Encode(report); err != nil {
			return 1
		}
		return 0
	}
	if command != "plan" && command != "sync" && command != "watch" && command != "recover" && command != "config" && command != "audit" {
		return usage(errOut)
	}
	if len(args) == 3 && !((command == "watch" && args[2] == "--apply") || (command == "audit" && args[2] == "--json")) {
		return usage(errOut)
	}
	last := ""
	watchBlocked := false
	lastObservation := ""
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
			if command == "watch" {
				lastObservation = ""
				if !retryWatch(ctx, errOut, &watchBlocked) {
					if ctx.Err() != nil {
						return 0
					}
					return 1
				}
				continue
			}
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
			if command == "watch" {
				lastObservation = ""
				if !retryWatch(ctx, errOut, &watchBlocked) {
					if ctx.Err() != nil {
						return 0
					}
					return 1
				}
				continue
			}
			fmt.Fprintln(errOut, err)
			return 1
		}
		if watchBlocked {
			if _, err := fmt.Fprintln(errOut, "Watch inputs are readable again; planning resumed. Conflicts still block writes."); err != nil {
				return 1
			}
			watchBlocked = false
			last = ""
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
		options := bridge.Options{}
		apply := command == "sync"
		if command == "watch" && len(args) == 3 {
			observation := bridge.Observation(c, result)
			apply = observation == lastObservation
			lastObservation = observation
			options.ExpectedObservation = observation
		}
		if !conflict && apply {
			if _, err = bridge.Apply(c, options); err != nil {
				if command == "watch" && errors.Is(err, bridge.ErrObservationChanged) {
					lastObservation = ""
				} else {
					fmt.Fprintln(errOut, err)
					return 1
				}
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

// Input errors can be temporary editor saves. Never apply or auto-recover while
// unreadable; emit a redacted transition once, then retry until canceled.
func retryWatch(ctx context.Context, out io.Writer, blocked *bool) bool {
	if !*blocked {
		if _, err := fmt.Fprintln(out, "Watch paused: configuration or inputs are unreadable/unsupported, or recovery is pending. No sync attempted; retrying. Use audit/plan to inspect."); err != nil {
			return false
		}
		*blocked = true
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func usage(w io.Writer) int {
	fmt.Fprintln(w, "       agent-bridge check-overlap <profile.json> <other.json> [more profiles...]")
	fmt.Fprintln(w, "Usage: agent-bridge <config|plan|sync|watch|recover|audit|init> <config.json> [--apply (watch only) | --json (audit only)]\n       agent-bridge <discover|watch-discovery> <root> <--project|--global>\n       agent-bridge service <install|start|stop|status|uninstall> <config.json> [--apply (install only)]")
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
