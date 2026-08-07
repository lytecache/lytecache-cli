package service

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrNotWindows is returned by every ScheduledTask* function outside
// Windows -- they exist on every platform purely so cmd_service.go can
// call them uniformly behind a runtime.GOOS == "windows" guard, without
// its own build-tagged files. Declared here (no build tag) rather than in
// scheduledtask_other.go (which is //go:build !windows) because
// elevation_test.go references it directly and must compile on every OS
// in the CI matrix, including windows-latest, even though the assertions
// that use it only run when GOOS != "windows".
var ErrNotWindows = errors.New("service: scheduled tasks are a Windows-only fallback")

// Method identifies which OS mechanism a `service install` actually used.
// On macOS/Linux this is always MethodOSService (kardianos/service's
// launchd/systemd integration always succeeds without elevation, since
// both are installed user-level -- see BuildConfig). On Windows it
// depends on whether the installing shell was elevated: MethodOSService
// (SCM) if so, MethodScheduledTask (the schtasks.exe fallback) if not --
// see scheduledtask_windows.go. The other `service` subcommands
// (start/stop/status/uninstall) need to know which one was used, since
// kardianos/service has no visibility into a Scheduled Task it didn't
// create.
type Method string

// The two Method values a `service install` can record.
const (
	MethodOSService     Method = "os-service"
	MethodScheduledTask Method = "scheduled-task"
)

func installRecordPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lytecache-ui.install-method"), nil
}

// WriteInstallRecord persists which Method `service install` used.
func WriteInstallRecord(m Method) error {
	path, err := installRecordPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(m), 0o644)
}

// ReadInstallRecord returns the persisted Method, defaulting to
// MethodOSService if no record exists (covers macOS/Linux, which never
// write one, and any install performed before this file existed).
func ReadInstallRecord() (Method, error) {
	path, err := installRecordPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return MethodOSService, nil
	}
	if err != nil {
		return "", err
	}
	return Method(data), nil
}

// RemoveInstallRecord deletes the persisted Method, if any. Called by
// `service uninstall` so a later reinstall doesn't inherit a stale record.
func RemoveInstallRecord() error {
	path, err := installRecordPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
