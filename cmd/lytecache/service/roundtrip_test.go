package service

import (
	"context"
	"runtime"
	"testing"

	kservice "github.com/kardianos/service"
)

// TestServiceInstallUninstallRoundTrip registers and removes a REAL
// OS-level service definition (a launchd LaunchAgent plist, a systemd
// user unit file, or a Windows SCM entry) -- the one thing a unit test
// against buildConfigForGOOS's *output* can't verify: that the real
// per-platform kardianos/service backend actually accepts what this
// package hands it and can undo it again. Skipped under -short, matching
// this repo's existing integration_test.go/ui_integration_test.go
// convention for "does something real, not just in-process" tests -- the
// CI workflow (lytecache-cli-ci.yml) runs without -short on all three
// OSes, so this exercises the real backend there without any workflow
// changes.
//
// Deliberately does NOT call Start/Stop: those would need a real,
// long-lived executable for the OS to launch (Install's default
// Executable is "the current executable" -- this test binary itself,
// which becomes invalid the moment `go test` exits), so exercising a
// live process this way is out of scope for an automated test; the
// documented manual verification for that is `lytecache service install
// && lytecache service start && lytecache service status` against the
// real CLI binary (see docs/ui.md).
//
// Uses a distinct, clearly-labeled test-only service name -- never the
// real Name constant -- so this can never collide with, or destroy, an
// operator's actual, intentionally-installed lytecache-ui service on the
// same machine.
func TestServiceInstallUninstallRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real OS service registration in -short mode")
	}

	cfg := buildConfigForGOOS(nil, false, runtime.GOOS)
	cfg.Name = "lytecache-ui-test-roundtrip"
	cfg.DisplayName = "lytecache ui test (safe to remove)"
	cfg.Description = "Created by cmd/lytecache/service's automated tests; removed at the end of the same test."

	prog := &Program{
		StartFunc: func() error { return nil },
		StopFunc:  func(context.Context) error { return nil },
	}
	svc, err := kservice.New(prog, cfg)
	if err != nil {
		t.Fatalf("kservice.New: %v", err)
	}

	// Cleanup runs even if the test fails partway through -- a failed
	// assertion must never leave the test registration behind.
	t.Cleanup(func() { _ = svc.Uninstall() })

	if err := svc.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Status queries the OS's own bookkeeping for this service -- safe to
	// call without ever having started it (expected: not running), and
	// itself proves the registration Install() just performed is
	// something the OS backend can actually see and answer about.
	if _, err := svc.Status(); err != nil {
		t.Errorf("Status after Install: %v", err)
	}

	if err := svc.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// A second Uninstall should now fail -- confirms the first one
	// actually removed the registration rather than silently no-op'ing.
	if err := svc.Uninstall(); err == nil {
		t.Error("expected a second Uninstall to fail (nothing left to remove) -- the first one may not have actually worked")
	}
}
