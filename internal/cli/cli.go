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
	if len(args) > 0 && (args[0] == "logs" || args[0] == "doctor" || args[0] == "support-bundle") {
		return runDiagnostics(ctx, args, out, errOut)
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
	if len(args) > 0 && args[0] == "compare-plugin-copy" {
		return runPluginCopy(args, out, errOut)
	}
	if len(args) > 0 && (args[0] == "review-retirement" || args[0] == "apply-retirement" || args[0] == "review-file-change" || args[0] == "apply-file-change" || args[0] == "recover-file-change" || args[0] == "file-change-history" || args[0] == "review-file-change-undo" || args[0] == "apply-file-change-undo") {
		return runFileChange(ctx, args, out, errOut)
	}
	if len(args) > 0 && (args[0] == "review-enrollment" || args[0] == "create-enrolled" || args[0] == "recover-enrollment") {
		return runEnrollmentCreation(ctx, args, out, errOut)
	}
	if len(args) > 0 && (args[0] == "review-resolution" || args[0] == "resolve-reviewed") {
		return runResolve(ctx, args, out, errOut)
	}
	if len(args) > 0 && (args[0] == "history" || args[0] == "review-history" || args[0] == "restore-reviewed") {
		return runHistory(ctx, args, out, errOut)
	}
	if len(args) > 0 && args[0] == "enroll-reviewed" {
		if len(args) != 3 {
			return usage(errOut)
		}
		if err := bridge.EnrollReviewed(args[1], args[2]); err != nil {
			event(ctx, "operation", "cli.enrollment", err)
			if errors.Is(err, bridge.ErrObservationChanged) {
				fmt.Fprintln(errOut, "Reviewed inputs changed; review again before enrolling.")
				return 2
			}
			fmt.Fprintln(errOut, "Enrollment failed; inspect review, roster ownership, overlaps and locks privately.")
			return 1
		}
		if _, err := fmt.Fprintln(out, "Enrolled the reviewed profile. Native files and synchronization baselines were not changed."); err != nil {
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "systemd-unit" {
		if len(args) < 3 || len(args) > 4 || (len(args) == 4 && args[3] != "--apply") {
			return usage(errOut)
		}
		unit, err := bridge.SystemdUnit(args[1], args[2], len(args) == 4)
		if err != nil {
			fmt.Fprintln(errOut, "Unit export failed: check absolute paths, executable, profile safety and conflicts. No service installed.")
			return 1
		}
		if _, err := fmt.Fprint(out, unit); err != nil {
			return 1
		}
		return 0
	}
	if len(args) > 0 && (args[0] == "review-profile" || args[0] == "sync-reviewed") {
		if args[0] == "review-profile" {
			if len(args) != 2 {
				return usage(errOut)
			}
			r, err := bridge.ReviewProfile(args[1])
			if err != nil {
				fmt.Fprintln(errOut, "Review failed: check profile safety, adapter support and conflicts. No files changed.")
				return 1
			}
			if err := json.NewEncoder(out).Encode(r); err != nil {
				return 1
			}
			return 0
		}
		if len(args) != 3 {
			return usage(errOut)
		}
		r, err := bridge.SyncReviewedObserved(args[1], args[2], observer(ctx))
		if err != nil {
			event(ctx, "operation", "cli", err)
			if errors.Is(err, bridge.ErrObservationChanged) {
				fmt.Fprintln(errOut, "Reviewed inputs changed; review again before syncing.")
				return 2
			}
			fmt.Fprintln(errOut, "Reviewed sync failed. Inspect the profile, ownership roster and pending recovery state privately.")
			return 1
		}
		if err := json.NewEncoder(out).Encode(r); err != nil {
			return 1
		}
		return 0
	}
	if len(args) > 0 && args[0] == "draft-profile" {
		if len(args) < 4 || (args[2] != "--global" && args[2] != "--project") {
			return usage(errOut)
		}
		scope := "project"
		if args[2] == "--global" {
			scope = "global"
		}
		draft, err := bridge.DraftProfile(args[1], scope, args[3:])
		if err != nil {
			fmt.Fprintln(errOut, "Could not draft profile: check the explicit root, scope, selected IDs and unsafe or ambiguous paths. No files changed.")
			return 1
		}
		if err := json.NewEncoder(out).Encode(draft); err != nil {
			return 1
		}
		return 0
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
		if len(args) != 2 && !(len(args) == 3 && args[2] == "--conventions") {
			return usage(errOut)
		}
		init := bridge.InitProfile
		message := "Created an empty private profile. Add reviewed resources, then audit and plan before syncing."
		if len(args) == 3 {
			init = bridge.InitConventionProfile
			message = "Created a private convention-based instruction profile for the containing project. Review exclusions, then audit and plan before syncing. No instruction files changed."
		}
		if err := init(filename); err != nil {
			fmt.Fprintln(errOut, "Could not create profile; check path safety, permissions, and whether it already exists.")
			return 1
		}
		if _, err := fmt.Fprintln(out, message); err != nil {
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
	lastConventionWarnings := ""
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
				if !watchBlocked {
					event(ctx, "watch_paused", "cli", err)
				}
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
			event(ctx, "config_load", "cli", err)
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
			// The resolved inventory includes convention warnings.
			if err = json.NewEncoder(out).Encode(c); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			return 0
		}
		warnings, _ := json.Marshal(c.ConventionWarnings)
		if string(warnings) != lastConventionWarnings {
			for _, warning := range c.ConventionWarnings {
				if _, err := fmt.Fprintln(errOut, "Conventions:", warning); err != nil {
					return 1
				}
			}
			lastConventionWarnings = string(warnings)
		}
		if command == "recover" {
			result, err := bridge.RecoverObserved(c, observer(ctx))
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
		result, err := bridge.PlanObserved(c, observer(ctx))
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
			event(ctx, "watch_resumed", "cli", nil)
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
		if conflict {
			event(ctx, "conflict", "cli", bridge.ErrConflicts)
		}
		options := bridge.Options{Observe: observer(ctx)}
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
	fmt.Fprintln(w, "       agent-bridge init <config.json> [--conventions]")
	fmt.Fprintln(w, "       agent-bridge logs <config.json> [--tail 1..1000]\n       agent-bridge doctor <config.json>\n       agent-bridge support-bundle <config.json> <new-output.json>")
	fmt.Fprintln(w, "       agent-bridge review-retirement <config.json> <resource-id>")
	fmt.Fprintln(w, "       agent-bridge apply-retirement <config.json> <observation> <resource-id>")
	fmt.Fprintln(w, "       agent-bridge enroll-reviewed <config.json> <observation>")
	fmt.Fprintln(w, "       agent-bridge review-enrollment <template.json> <absolute-new-profile>")
	fmt.Fprintln(w, "       agent-bridge create-enrolled <template.json> <absolute-new-profile> <observation>")
	fmt.Fprintln(w, "       agent-bridge recover-enrollment <absolute-new-profile> <absolute-coordination-dir>")
	fmt.Fprintln(w, "       agent-bridge review-resolution <config.json> <item-key=side>...")
	fmt.Fprintln(w, "       agent-bridge resolve-reviewed <config.json> <observation> <item-key=side>...")
	fmt.Fprintln(w, "       agent-bridge history <config.json>")
	fmt.Fprintln(w, "       agent-bridge review-history <config.json> <transaction> <item-key> <side> <before|after>")
	fmt.Fprintln(w, "       agent-bridge review-file-change <config.json> <item-key> <--delete|--rename relative-path>")
	fmt.Fprintln(w, "       agent-bridge apply-file-change <config.json> <observation> <item-key> <--delete|--rename relative-path>")
	fmt.Fprintln(w, "       agent-bridge recover-file-change <config.json>")
	fmt.Fprintln(w, "       agent-bridge file-change-history <config.json>")
	fmt.Fprintln(w, "       agent-bridge review-file-change-undo <config.json> <transaction>")
	fmt.Fprintln(w, "       agent-bridge apply-file-change-undo <config.json> <observation> <transaction>")
	fmt.Fprintln(w, "       agent-bridge compare-plugin-copy <config.json> <plugin-id> <claude|codex> <absolute-copy-root>")
	fmt.Fprintln(w, "       agent-bridge restore-reviewed <config.json> <observation> <transaction> <item-key> <side> <before|after>")
	fmt.Fprintln(w, "       agent-bridge systemd-unit <absolute-config.json> <absolute-binary> [--apply]")
	fmt.Fprintln(w, "       agent-bridge review-profile <config.json>\n       agent-bridge sync-reviewed <config.json> <observation>")
	fmt.Fprintln(w, "       agent-bridge draft-profile <root> <--project|--global> <candidate-id> [more IDs...]")
	fmt.Fprintln(w, "       agent-bridge check-overlap <profile.json> <other.json> [more profiles...]")
	fmt.Fprintln(w, "Usage: agent-bridge <config|plan|sync|watch|recover|audit|init> <config.json> [--apply (watch only) | --json (audit only)]\n       agent-bridge <discover|watch-discovery> <root> <--project|--global>\n       agent-bridge service <install|start|stop|status|uninstall> <config.json> [--apply (install only)]")
	return 1
}

func writeAudit(w io.Writer, report bridge.AuditReport) error {
	if _, err := fmt.Fprintln(w, "Compatibility audit (read-only; host behavior not verified)"); err != nil {
		return err
	}
	for _, warning := range report.ConventionWarnings {
		if _, err := fmt.Fprintln(w, "Conventions:", warning); err != nil {
			return err
		}
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
