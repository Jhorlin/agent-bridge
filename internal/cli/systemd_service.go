package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Jhorlin/agent-bridge/internal/bridge"
)

func runLinuxService(ctx context.Context, args []string, out, errOut io.Writer) int {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 1
		}
		configHome = filepath.Join(home, ".config")
	}
	binary, err := os.Executable()
	if err != nil {
		return 1
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return 1
	}
	return runLinuxServiceWith(ctx, args, configHome, binary, out, errOut, bridge.Systemctl)
}

func runLinuxServiceWith(ctx context.Context, args []string, configHome, binary string, out, errOut io.Writer, run bridge.SystemdRunner) int {
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
	s, err := bridge.NewSystemdService(profile, configHome)
	if err != nil {
		fmt.Fprintln(errOut, "Unsafe systemd service paths.")
		return 1
	}
	if action == "status" {
		status, err := s.Status(ctx, run)
		if err != nil {
			fmt.Fprintln(errOut, "Service state unavailable; inspect owned files and the user manager.")
			return 1
		}
		if err := json.NewEncoder(out).Encode(status); err != nil {
			return 1
		}
		return 0
	}
	switch action {
	case "install":
		var c bridge.Config
		c, err = bridge.LoadAuditConfig(profile)
		if err == nil {
			err = s.Install(c, binary, len(args) == 3)
		}
	case "start":
		err = s.Start(ctx, run)
	case "stop":
		err = s.Stop(ctx, run)
	case "uninstall":
		err = s.Uninstall(ctx, run)
	}
	if err != nil {
		fmt.Fprintln(errOut, "Systemd operation failed; inspect ownership, locks, overrides and the user manager. Partial changes may require inspection.")
		return 1
	}
	message := "Systemd service " + action + " completed."
	if action == "install" {
		message = "Installed owned user-unit files for the next user-manager login. Run service start for this session. Preview-only unless installed with --apply."
	}
	if action == "stop" {
		message = "Stopped the service; login enablement remains."
	}
	if action == "uninstall" {
		message = "Removed unchanged owned unit, enable link and receipt. Native files, state, backups and journal logs were preserved."
	}
	if _, err := fmt.Fprintln(out, message); err != nil {
		return 1
	}
	return 0
}
