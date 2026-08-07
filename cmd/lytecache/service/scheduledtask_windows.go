//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// InstallScheduledTask registers a Windows Scheduled Task that runs exe
// with args at user logon -- the non-elevated fallback for `service
// install` when SCM registration isn't available (SCM requires
// administrator rights; a per-user logon task doesn't).
// kardianos/service has no Scheduled Task support of its own (only SCM
// on Windows), so this shells out to schtasks.exe directly -- the
// standard, always-present tool for this on every supported Windows
// version, rather than a hand-rolled Task Scheduler COM/XML integration.
func InstallScheduledTask(name string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	action := quoteArg(exe)
	for _, a := range args {
		action += " " + quoteArg(a)
	}

	// /RL LIMITED: runs with the logged-on user's normal (non-elevated)
	// rights -- matches what a foreground `lytecache ui` would have run
	// as anyway. /F: overwrite a previous registration under this name,
	// so re-running install after a config change doesn't fail on
	// "already exists".
	out, err := exec.Command("schtasks", "/Create", "/TN", name, "/TR", action, "/SC", "ONLOGON", "/RL", "LIMITED", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create failed: %w\n%s", err, out)
	}
	return nil
}

func UninstallScheduledTask(name string) error {
	out, err := exec.Command("schtasks", "/Delete", "/TN", name, "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Delete failed: %w\n%s", err, out)
	}
	return nil
}

func StartScheduledTask(name string) error {
	out, err := exec.Command("schtasks", "/Run", "/TN", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Run failed: %w\n%s", err, out)
	}
	return nil
}

func StopScheduledTask(name string) error {
	out, err := exec.Command("schtasks", "/End", "/TN", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /End failed: %w\n%s", err, out)
	}
	return nil
}

// ScheduledTaskStatus reports whether the named task is currently running,
// via `schtasks /Query`'s parseable CSV output.
func ScheduledTaskStatus(name string) (running bool, err error) {
	out, err := exec.Command("schtasks", "/Query", "/TN", name, "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("schtasks /Query failed: %w\n%s", err, out)
	}
	return strings.Contains(string(out), ",\"Running\","), nil
}

func quoteArg(a string) string {
	return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
}
