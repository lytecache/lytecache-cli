//go:build !windows

package service

// IsElevated is meaningless outside Windows SCM registration -- macOS/
// Linux service installs are user-level by default (see BuildConfig),
// requiring no elevated privileges at all. Always true here so callers
// that only need to gate the Windows-specific path don't need their own
// runtime.GOOS check first.
func IsElevated() bool { return true }
