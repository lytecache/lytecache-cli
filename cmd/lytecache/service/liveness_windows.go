//go:build windows

package service

import "os"

// processAlive reports whether pid is a live process. On Windows,
// os.FindProcess itself opens a handle to the process (OpenProcess) and
// fails if it doesn't exist -- unlike Unix, there's no separate liveness
// probe needed, and Signal(0) is not portably supported on Windows
// processes anyway.
func processAlive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
