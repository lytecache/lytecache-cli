//go:build windows

package ui

import "os"

// checkNotGroupOrWorldAccessible always reports "fine" on Windows.
// os.Chmod/os.WriteFile can't express POSIX owner/group/other bits there
// at all -- Go's os package only toggles the read-only attribute, so
// Stat().Mode().Perm() reports 0666 for any normal writable file
// regardless of what mode it was created with. Enforcing configFileMode's
// 0600 guarantee via permission bits is simply not possible on this
// platform; the real protection here is NTFS ACLs on the per-user
// %APPDATA% directory ui.yaml lives under, which already restrict access
// to the owning account (plus Administrators) by default -- the same
// trade-off tools like kubectl and the AWS CLI make for their own
// per-user credential files on Windows.
func checkNotGroupOrWorldAccessible(_ os.FileInfo) bool {
	return false
}
