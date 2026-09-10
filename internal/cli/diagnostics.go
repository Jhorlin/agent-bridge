package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
)

type observerKey struct{}

func observer(ctx context.Context) diagnostics.Observer {
	sink, _ := ctx.Value(observerKey{}).(diagnostics.Observer)
	return sink
}
func event(ctx context.Context, stage, component string, err error) {
	if sink := observer(ctx); sink != nil {
		code := bridge.DiagnosticCode(err)
		sink(diagnostics.Event{Stage: stage, Component: component, Code: code})
	}
}
func logDirectory(profile string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return diagnostics.Directory(profile, home, os.Getenv("XDG_STATE_HOME"), runtime.GOOS)
}
func loggedCommand(args []string) (command, profile string) {
	if len(args) < 2 {
		return "", ""
	}
	command, profile = args[0], args[1]
	if command == "service" {
		if len(args) < 3 || !diagnostics.Member(args[1], "install|start|stop|uninstall") {
			return "", ""
		}
		return "service-" + args[1], args[2]
	}
	if command == "watch" {
		if len(args) != 3 || args[2] != "--apply" {
			return "", ""
		}
		return command, profile
	}
	if command == "create-enrolled" {
		if len(args) < 3 {
			return "", ""
		}
		profile = args[2]
	}
	if !diagnostics.Member(command, "sync|sync-reviewed|recover|init|enroll-reviewed|create-enrolled|recover-enrollment|resolve-reviewed|restore-reviewed|apply-retirement|apply-file-change|recover-file-change|apply-file-change-undo") {
		return "", ""
	}
	return command, profile
}
func overlaps(a, b string) bool {
	within := func(root, path string) bool {
		rel, err := filepath.Rel(root, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return within(a, b) || within(b, a)
}
func protectedPaths(c bridge.Config) []string {
	p := append([]string{c.StateDir}, c.ConfigFiles...)
	if c.CoordinationDir != "" {
		p = append(p, c.CoordinationDir)
	}
	for _, r := range c.Resources {
		for _, path := range r.Paths {
			p = append(p, path)
		}
		if path := bridge.InstructionCompanionPath(r); path != "" {
			p = append(p, path)
		}
		for _, path := range r.CodexAgentExports {
			p = append(p, path)
		}
		for _, link := range r.Links {
			p = append(p, link.Path, link.Target)
		}
	}
	return p
}
func diagnosticOverlap(profile, dir string) bool {
	abs, err := filepath.Abs(profile)
	if err != nil || overlaps(abs, dir) {
		return true
	}
	if paths, err := bridge.DiagnosticProtectedPaths(profile); err == nil {
		for _, p := range paths {
			if p != "" && overlaps(p, dir) {
				return true
			}
		}
	}
	return false
}

// RunLogged is the executable entry point. Run remains useful for embedded callers
// that do not opt into persistence. Preview/read-only commands create no logs.
func RunLogged(ctx context.Context, args []string, out, errOut io.Writer) (exit int) {
	command, profile := loggedCommand(args)
	if command == "" || ctx.Err() != nil {
		return Run(ctx, args, out, errOut)
	}
	dir, err := logDirectory(profile)
	if err != nil || diagnosticOverlap(profile, dir) {
		fmt.Fprintln(errOut, "Diagnostic logging unavailable (log_path_unsafe); operation continues.")
		return Run(ctx, args, out, errOut)
	}
	logger, err := diagnostics.New(dir, profile, command, Version, errOut)
	if err != nil {
		fmt.Fprintln(errOut, "Diagnostic logging unavailable (log_init_failed); operation continues.")
		return Run(ctx, args, out, errOut)
	}
	sink := diagnostics.Observer(func(e diagnostics.Event) {
		// Applying watchers reload profiles: stop logging if a later edit enrolls logs.
		if diagnosticOverlap(profile, dir) {
			logger.Disable()
			return
		}
		logger.Emit(e)
	})
	ctx = context.WithValue(ctx, observerKey{}, sink)
	start := time.Now()
	event(ctx, "command_start", "cli", nil)
	defer func() {
		if p := recover(); p != nil {
			sink(diagnostics.Event{Stage: "panic", Component: "cli", Code: "panic"})
			// Never persist panic text/stack (may contain configuration); preserve pending state.
			fmt.Fprintln(errOut, "Unexpected internal failure (panic); inspect doctor and pending recovery before retrying.")
			exit = 1
		}
		code := "ok"
		if exit == 2 {
			code = "blocked"
		} else if exit != 0 {
			code = "operation_failed"
		}
		sink(diagnostics.Event{Stage: "command_finish", Component: "cli", Code: code, DurationMS: time.Since(start).Milliseconds(), Exit: exit})
	}()
	return Run(ctx, args, out, errOut)
}

type diagnosticReport struct {
	Schema    int                    `json:"schema"`
	Time      string                 `json:"time"`
	Version   string                 `json:"version"`
	Revision  string                 `json:"revision,omitempty"`
	Dirty     bool                   `json:"dirty,omitempty"`
	Platform  string                 `json:"platform"`
	Profile   string                 `json:"profileRef"`
	Config    string                 `json:"config"`
	Resources int                    `json:"resourceCount"`
	Plan      string                 `json:"plan"`
	State     map[string]string      `json:"state"`
	Service   diagnosticService      `json:"service"`
	Logs      diagnostics.ReadResult `json:"logs"`
}

type diagnosticService struct {
	Status    string `json:"status"`
	Installed *bool  `json:"installed,omitempty"`
	Apply     *bool  `json:"apply,omitempty"`
}

// State discovery uses metadata only; lock contents, manifests and snapshots are excluded.
func stateStatus(path string) string {
	if err := diagnostics.Safe(path); err != nil {
		return "unsafe"
	}
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		return "unavailable"
	}
	if !fi.Mode().IsRegular() {
		return "unsafe"
	}
	return "present"
}
func serviceStatus(ctx context.Context, profile string) diagnosticService {
	unavailable := diagnosticService{Status: "unavailable"}
	home, err := os.UserHomeDir()
	if err != nil {
		return unavailable
	}
	var status bridge.ServiceStatus
	if runtime.GOOS == "darwin" {
		s, e := bridge.NewLaunchService(profile, home, os.Getuid())
		if e != nil {
			return unavailable
		}
		status, err = s.Status(ctx, bridge.Launchctl)
	} else if runtime.GOOS == "linux" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		s, e := bridge.NewSystemdService(profile, base)
		if e != nil {
			return unavailable
		}
		status, err = s.Status(ctx, bridge.Systemctl)
	} else {
		return diagnosticService{Status: "unsupported"}
	}
	if err != nil {
		return unavailable
	}
	if !diagnostics.Member(status.Registration, "not-installed|unknown|unregistered|registered|active|inactive|failed|activating|deactivating|reloading") {
		return diagnosticService{Status: "unknown", Installed: &status.Installed, Apply: &status.Apply}
	}
	return diagnosticService{Status: status.Registration, Installed: &status.Installed, Apply: &status.Apply}
}
func inspectDiagnostics(ctx context.Context, profile, dir string, tail int, service func(context.Context, string) diagnosticService) (diagnosticReport, map[string]string, []string) {
	abs, _ := filepath.Abs(profile)
	r := diagnosticReport{Schema: 1, Time: time.Now().UTC().Format(time.RFC3339Nano), Version: diagnostics.BuildVersion(Version), Platform: diagnostics.Platform(), Profile: diagnostics.Ref(abs), Config: "ok", Plan: "not_checked", State: map[string]string{}, Service: service(ctx, profile), Logs: diagnostics.Read(dir, tail)}
	r.Revision, r.Dirty = diagnostics.Revision()
	mapping := map[string]string{diagnostics.Ref(abs): abs}
	c, err := bridge.LoadAuditConfig(profile)
	if err != nil {
		r.Config = diagnostics.Code(err)
		return r, mapping, []string{abs}
	}
	r.Resources = len(c.Resources)
	paths := protectedPaths(c)
	for _, p := range paths {
		if p != "" {
			mapping[diagnostics.Ref(p)] = p
		}
	}
	for _, resource := range c.Resources {
		mapping[diagnostics.Ref(resource.ID)] = resource.ID
	}
	for key, name := range map[string]string{"syncLock": "sync.lock", "syncPending": "pending.json", "fileChangePending": "file-change-pending.json", "manifest": "manifest.json"} {
		path := filepath.Join(c.StateDir, name)
		r.State[key] = stateStatus(path)
		mapping[diagnostics.Ref(path)] = path
	}
	if c.CoordinationDir != "" {
		for key, name := range map[string]string{"coordinationLock": "sync.lock", "enrollmentPending": "enrollment-pending.json"} {
			path := filepath.Join(c.CoordinationDir, name)
			r.State[key] = stateStatus(path)
			mapping[diagnostics.Ref(path)] = path
		}
	}
	plan, err := bridge.Plan(c)
	r.Plan = bridge.DiagnosticCode(err)
	if err == nil && plan.HasConflicts() {
		r.Plan = "conflict"
	}
	for _, item := range plan.Items {
		for _, p := range item.Paths {
			mapping[diagnostics.Ref(p)] = p
		}
		if p := bridge.InstructionCompanionPath(item.Resource); p != "" {
			mapping[diagnostics.Ref(p)] = p
		}
	}
	return r, mapping, paths
}
func runDiagnostics(ctx context.Context, args []string, out, errOut io.Writer) int {
	return runDiagnosticsWith(ctx, args, out, errOut, serviceStatus)
}
func runDiagnosticsWith(ctx context.Context, args []string, out, errOut io.Writer, service func(context.Context, string) diagnosticService) int {
	if len(args) < 2 {
		return usage(errOut)
	}
	command, profile := args[0], args[1]
	tail := 50
	if command == "logs" {
		if len(args) != 2 && !(len(args) == 4 && args[2] == "--tail") {
			return usage(errOut)
		}
		if len(args) == 4 {
			n, e := strconv.Atoi(args[3])
			if e != nil || n < 1 || n > 1000 {
				return usage(errOut)
			}
			tail = n
		}
	} else if command == "doctor" {
		if len(args) != 2 {
			return usage(errOut)
		}
	} else if command == "support-bundle" {
		if len(args) != 3 {
			return usage(errOut)
		}
		tail = 200
	} else {
		return usage(errOut)
	}
	dir, err := logDirectory(profile)
	if err != nil {
		fmt.Fprintln(errOut, "Cannot locate private diagnostic directory.")
		return 1
	}
	r, mapping, paths := inspectDiagnostics(ctx, profile, dir, tail, service)
	if command == "support-bundle" {
		// Invalid config cannot establish safe output boundaries: fail closed.
		if r.Config != "ok" {
			fmt.Fprintln(errOut, "Bundle requires a valid profile to protect managed paths; doctor remains available.")
			return 1
		}
		target, e := filepath.Abs(args[2])
		if e != nil {
			return 1
		}
		for _, p := range append(paths, dir) {
			if overlaps(p, target) {
				fmt.Fprintln(errOut, "Bundle destination overlaps protected data.")
				return 1
			}
		}
		data, e := json.MarshalIndent(r, "", "  ")
		if e == nil {
			e = diagnostics.WriteBundle(target, append(data, '\n'))
		}
		if e != nil {
			fmt.Fprintln(errOut, "Bundle write failed; inspect the destination before retrying. Existing files were not overwritten.")
			return 1
		}
		_, err = fmt.Fprintln(out, "Created sanitized diagnostic bundle. Review before sharing; backups, configuration and local reference map are excluded.")
	} else if command == "logs" {
		// Deliberately local-only: never embed this reference map into a bundle.
		err = json.NewEncoder(out).Encode(struct {
			Directory  string                 `json:"directory"`
			NativeLogs map[string]string      `json:"nativeLogsLocalOnly"`
			References map[string]string      `json:"localReferences"`
			Logs       diagnostics.ReadResult `json:"logs"`
		}{dir, nativeLogLocations(profile), mapping, r.Logs})
	} else {
		err = json.NewEncoder(out).Encode(r)
	}
	if err != nil {
		return 1
	}
	return 0
}

func nativeLogLocations(profile string) map[string]string {
	r := map[string]string{}
	abs, e := filepath.Abs(profile)
	if e != nil {
		return r
	}
	short := diagnostics.Ref(abs)[:32]
	if runtime.GOOS == "darwin" {
		home, e := os.UserHomeDir()
		if e != nil {
			return r
		}
		base := filepath.Join(home, "Library", "LaunchAgents", ".agent-bridge", "com.jhorlin.agent-bridge."+short)
		r["stdout"] = base + ".out.log"
		r["stderr"] = base + ".err.log"
	} else if runtime.GOOS == "linux" {
		r["journalCommand"] = "journalctl --user -u agent-bridge-" + short + ".service --since today"
	}
	return r
}
