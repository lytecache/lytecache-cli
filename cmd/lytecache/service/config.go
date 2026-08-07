// Package service wraps github.com/kardianos/service to run `lytecache
// ui` as a managed background service on macOS (launchd), Linux (systemd,
// with Upstart/OpenRC/SysV/procd detected automatically as a fallback by
// kardianos/service itself), and Windows (Service Control Manager, with a
// Scheduled Task fallback when not running elevated -- see
// scheduledtask_windows.go). This package does not hand-roll any
// per-platform daemonization: kardianos/service already generates the
// launchd plist / systemd unit / Windows service registration; this
// package only chooses the right options (user vs. system scope, restart
// policy) and supplies the generic Start/Stop plumbing (program.go) and
// log path resolution (logs.go).
package service

import (
	"runtime"

	kservice "github.com/kardianos/service"
)

// Name is the service's internal name, used by the OS service manager
// (systemd unit name, launchd label, Windows service name) -- must stay
// stable across versions, since installing under a different name would
// orphan a previous install rather than replace it.
const Name = "lytecache-ui"

// DisplayName is the human-readable name shown by the OS service manager
// (e.g. `systemctl status`, launchd's label in Activity Monitor).
const DisplayName = "lytecache UI"

// Description is the short description the OS service manager stores
// alongside the service registration.
const Description = "Local web administration UI for lytecache database files (not a cache server)."

// BuildConfig builds the kardianos/service.Config for the lytecache-ui
// service. args are the exact `lytecache ui ...` flags to persist (the
// caller appends --as-service) -- see docs/ui.md's "flags passed to
// service install are persisted" guarantee: the daemon must start
// identically every time the OS restarts it, so everything it needs comes
// from these persisted arguments plus the config file they point at, not
// from any other ambient state.
//
// systemWide selects, on Linux only, a system-level systemd unit
// (/etc/systemd/system, requires root, survives across users) instead of
// the default user-level one (~/.config/systemd/user, no root, runs as
// whichever user installed it) -- see --system in cmd_service.go. macOS
// has no such flag: a LaunchAgent (user-level) is always used, matching
// the spec's explicit "no sudo" requirement for macOS. Windows has no
// user-level service concept at all; SCM registration is inherently
// system-wide (see scheduledtask_windows.go for the non-elevated
// fallback).
func BuildConfig(args []string, systemWide bool) *kservice.Config {
	return buildConfigForGOOS(args, systemWide, runtime.GOOS)
}

// buildConfigForGOOS is BuildConfig with the OS parameterized, so
// config_test.go can verify all three platforms' option sets from a
// single test run regardless of which OS actually runs `go test` --
// runtime.GOOS is otherwise fixed for the life of the test binary.
func buildConfigForGOOS(args []string, systemWide bool, goos string) *kservice.Config {
	cfg := &kservice.Config{
		Name:        Name,
		DisplayName: DisplayName,
		Description: Description,
		Arguments:   args,
		Option:      kservice.KeyValue{},
	}

	switch goos {
	case "darwin":
		cfg.Option["UserService"] = true
		cfg.Option["KeepAlive"] = true
		cfg.Option["RunAtLoad"] = true
	case "linux":
		cfg.Option["UserService"] = !systemWide
		cfg.Option["Restart"] = "on-failure"
	}
	return cfg
}
