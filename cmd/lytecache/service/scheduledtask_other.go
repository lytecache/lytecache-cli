//go:build !windows

package service

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
