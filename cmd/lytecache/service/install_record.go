package service

import (
	"errors"
	"os"
	"path/filepath"
)

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
