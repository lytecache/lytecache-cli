//go:build !windows

package service

import "errors"

// ErrNotWindows is returned by every ScheduledTask* function outside
// Windows -- they exist on every platform purely so cmd_service.go can
// call them uniformly behind a runtime.GOOS == "windows" guard, without
// its own build-tagged files.
var ErrNotWindows = errors.New("service: scheduled tasks are a Windows-only fallback")

// InstallScheduledTask always returns ErrNotWindows outside Windows; see
// scheduledtask_windows.go for the real implementation.
func InstallScheduledTask(_ string, _ []string) error { return ErrNotWindows }

// UninstallScheduledTask always returns ErrNotWindows outside Windows; see
// scheduledtask_windows.go for the real implementation.
func UninstallScheduledTask(_ string) error { return ErrNotWindows }

// StartScheduledTask always returns ErrNotWindows outside Windows; see
// scheduledtask_windows.go for the real implementation.
func StartScheduledTask(_ string) error { return ErrNotWindows }

// StopScheduledTask always returns ErrNotWindows outside Windows; see
// scheduledtask_windows.go for the real implementation.
func StopScheduledTask(_ string) error { return ErrNotWindows }

// ScheduledTaskStatus always returns ErrNotWindows outside Windows; see
// scheduledtask_windows.go for the real implementation.
func ScheduledTaskStatus(_ string) (bool, error) { return false, ErrNotWindows }
