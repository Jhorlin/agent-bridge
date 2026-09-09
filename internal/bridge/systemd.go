package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func systemdQuote(value string, command bool) string {
	value = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%").Replace(value)
	if command {
		value = strings.ReplaceAll(value, "$", "$$")
	}
	return "\"" + value + "\""
}

// SystemdUnit exports inert text only. Installing/enabling it is not implemented.
// Paths must refer to the intended Linux filesystem; no shell is involved.
func SystemdUnit(profile, binary string, apply bool) (string, error) {
	for _, path := range []string{profile, binary} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !validServicePath(path) {
			return "", fmt.Errorf("unit export requires clean absolute paths")
		}
		if err := assertSafe(path); err != nil {
			return "", err
		}
	}
	// The executable token has stricter systemd parsing than arguments. Keep
	// variable/specifier syntax out of it rather than relying on expansion rules.
	if strings.ContainsAny(binary, "$%") {
		return "", fmt.Errorf("executable path cannot contain variable or specifier syntax")
	}
	info, err := os.Stat(binary)
	if err != nil {
		return "", err
	}
	if err := checkRegular(info); err != nil {
		return "", err
	}
	if info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("binary is not executable")
	}
	c, err := LoadAuditConfig(profile)
	if err != nil {
		return "", fmt.Errorf("unit profile is invalid or unsafe")
	}
	plan, err := Plan(c)
	if err != nil {
		return "", fmt.Errorf("unit profile cannot be planned")
	}
	if apply && plan.HasConflicts() {
		return "", fmt.Errorf("conflicts block applying unit export")
	}
	args := systemdQuote(binary, true) + " watch " + systemdQuote(profile, true)
	if apply {
		args += " --apply"
	}
	return "# Agent Bridge user unit; export only, not installed.\n" +
		"[Unit]\nDescription=Agent Bridge configuration watcher\n\n" +
		"[Service]\nType=exec\nExecStart=" + args + "\n" +
		"UMask=0077\nRestart=no\nKillSignal=SIGTERM\nTimeoutStopSec=infinity\n" +
		"StandardOutput=journal\nStandardError=journal\n\n[Install]\nWantedBy=default.target\n", nil
}
