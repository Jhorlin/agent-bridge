package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runService(ctx context.Context, args []string, out, errOut io.Writer) int {
	return runServiceWith(ctx, args, out, errOut, bridge.Launchctl)
}

func runServiceWith(ctx context.Context, args []string, out, errOut io.Writer, run bridge.LaunchRunner) int {
	if len(args) < 2 || len(args) > 3 {
		return serviceUsage(errOut)
	}
	action, profile := args[0], args[1]
	if action != "install" && action != "start" && action != "stop" && action != "status" && action != "uninstall" {
		return serviceUsage(errOut)
	}
	if len(args) == 3 && !(action == "install" && args[2] == "--apply") {
		return serviceUsage(errOut)
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(errOut, "Background service management currently supports macOS only. Use watch on this platform.")
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(errOut, "Could not resolve service home.")
		return 1
	}
	service, err := bridge.NewLaunchService(profile, home, os.Getuid())
	if err != nil {
		fmt.Fprintln(errOut, "Unsafe service path.")
		return 1
	}
	if action == "status" {
		status, err := service.Status(ctx, run)
		if err != nil {
			fmt.Fprintln(errOut, "Service status unavailable; inspect service files and registration.")
			return 1
		}
		if err = json.NewEncoder(out).Encode(status); err != nil {
			return 1
		}
		return 0
	}
	switch action {
	case "install":
		var c bridge.Config
		c, err = bridge.LoadAuditConfig(profile)
		if err == nil {
			var plan bridge.PlanResult
			plan, err = bridge.Plan(c)
			if err == nil && len(args) == 3 && plan.HasConflicts() {
				err = fmt.Errorf("conflicts must be reconciled before installing an applying service")
			}
		}
		if err == nil {
			var binary string
			binary, err = os.Executable()
			if err == nil {
				binary, err = filepath.EvalSymlinks(binary)
			}
			if err == nil {
				err = service.Install(c, binary, len(args) == 3)
			}
		}
	case "start":
		err = service.Start(ctx, run)
	case "stop":
		err = service.Stop(ctx, run)
	case "uninstall":
		err = service.Uninstall(ctx, run)
	}
	if err != nil {
		fmt.Fprintln(errOut, "Service operation failed. Existing files were not overwritten; inspect ownership, locks, paths and launchd registration before retrying.")
		return 1
	}
	message := "Service " + action + " completed."
	if action == "install" {
		message = "Installed login service files. Run service start for this session. Preview-only unless installed with --apply; use a stable binary path."
	}
	if action == "stop" {
		message = "Service unregistered for this session; login installation remains. Use uninstall to remove it."
	}
	if action == "uninstall" {
		message = "Removed unchanged owned service files, if installed. Native files, synchronization state, backups and logs were preserved."
	}
	if _, err = fmt.Fprintln(out, message); err != nil {
		return 1
	}
	return 0
}
func serviceUsage(out io.Writer) int {
	fmt.Fprintln(out, "Usage: agent-bridge service <install|start|stop|status|uninstall> PROFILE [--apply (install only)]")
	return 1
}
