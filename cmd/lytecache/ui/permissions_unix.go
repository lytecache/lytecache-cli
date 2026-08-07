//go:build !windows

package ui

import "os"

// checkNotGroupOrWorldAccessible enforces configFileMode's owner-only
// guarantee using POSIX permission bits.
func checkNotGroupOrWorldAccessible(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 != 0
}
