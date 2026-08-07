//go:build windows

package service

import "golang.org/x/sys/windows"

// IsElevated reports whether the current process is running with
// administrator privileges -- required for Windows SCM service
// registration. Checks the process token's elevation state directly,
// which is the standard approach since UAC (Vista+): simpler and more
// reliable than checking Administrators-group membership, which doesn't
// account for UAC's split-token behavior (an admin's normal shell token
// is deliberately not elevated).
func IsElevated() bool {
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return false
	}
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}
