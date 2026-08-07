package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State records a running instance's own runtime info -- PID, start
// time, bound address, config path -- so `lytecache service status` can
// report them. None of the three platforms' service managers expose
// these uniformly through a single portable API, so the running process
// writes this itself at startup (WriteState) and removes it at clean
// shutdown (RemoveState); `service status` combines it with
// kardianos/service's own Status() (which does reliably answer
// running-or-not per platform) to detect a stale file from a process that
// died without cleaning up (see IsStale).
type State struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	Addr       string    `json:"addr"`
	ConfigPath string    `json:"config_path"`
}

// StatePath returns LogDir()/lytecache-ui.state.json -- alongside the log
// file, since both describe the same running instance and an operator
// looking at one will naturally look for the other in the same place.
func StatePath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lytecache-ui.state.json"), nil
}

// WriteState persists s, creating the log directory if needed. Called
// once the UI server is actually listening.
func WriteState(s State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ReadState loads the persisted State, if any.
func ReadState() (State, error) {
	path, err := StatePath()
	if err != nil {
		return State{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// RemoveState deletes the state file, if present. Called on graceful
// shutdown; a leftover file from an unclean exit is treated as stale (see
// IsStale) rather than an error condition anywhere that reads it.
func RemoveState() error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// IsStale reports whether s.PID no longer refers to a live process. See
// liveness_unix.go/liveness_windows.go: Unix's os.FindProcess always
// "succeeds" regardless of whether the process exists, so liveness needs
// signal 0; Windows' os.FindProcess actually opens a handle and fails on
// its own, so the two platforms need genuinely different checks, not just
// different syscall numbers.
func IsStale(s State) bool {
	return s.PID <= 0 || !processAlive(s.PID)
}
