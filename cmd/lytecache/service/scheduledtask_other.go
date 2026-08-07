//go:build !windows

package service

import "errors"

// ErrNotWindows is returned by every ScheduledTask* function outside
// Windows -- they exist on every platform purely so cmd_service.go can
// call them uniformly behind a runtime.GOOS == "windows" guard, without
// its own build-tagged files.
var ErrNotWindows = errors.New("service: scheduled tasks are a Windows-only fallback")

func InstallScheduledTask(_ string, _ []string) error { return ErrNotWindows }
func UninstallScheduledTask(_ string) error           { return ErrNotWindows }
func StartScheduledTask(_ string) error               { return ErrNotWindows }
func StopScheduledTask(_ string) error                { return ErrNotWindows }
func ScheduledTaskStatus(_ string) (bool, error)      { return false, ErrNotWindows }
