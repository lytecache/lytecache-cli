//go:build !windows

package service

import (
	"os"
	"syscall"
)

// processAlive reports whether pid is a live process. On Unix,
// os.FindProcess always "succeeds" regardless of whether the process
// exists -- signaling it with 0 (which delivers no actual signal) is what
// probes liveness for real, per kill(2).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
