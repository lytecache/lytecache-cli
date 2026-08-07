package service

import (
	"errors"
	"runtime"
	"testing"
)

// TestNonWindowsElevationAndScheduledTaskStubs exercises the portable
// stubs directly on whichever OS runs `go test` (macOS/Linux in this
// repo's CI matrix) -- the real Windows implementations
// (elevation_windows.go, scheduledtask_windows.go) are exercised on
// windows-latest by the same test binary, since Go only compiles the
// build-tag-matching file for the OS actually running the test; there is
// no way to invoke the *other* OS's file from here, which is exactly why
// the CI workflow runs this package's tests on all three OSes rather than
// relying on cross-compilation alone.
func TestNonWindowsElevationAndScheduledTaskStubs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this test asserts the non-Windows stub behavior specifically")
	}

	if !IsElevated() {
		t.Error("IsElevated() must always report true outside Windows (service installs are user-level, needing no elevation)")
	}

	if err := InstallScheduledTask("x", nil); !errors.Is(err, ErrNotWindows) {
		t.Errorf("InstallScheduledTask() = %v, want ErrNotWindows", err)
	}
	if err := UninstallScheduledTask("x"); !errors.Is(err, ErrNotWindows) {
		t.Errorf("UninstallScheduledTask() = %v, want ErrNotWindows", err)
	}
	if err := StartScheduledTask("x"); !errors.Is(err, ErrNotWindows) {
		t.Errorf("StartScheduledTask() = %v, want ErrNotWindows", err)
	}
	if err := StopScheduledTask("x"); !errors.Is(err, ErrNotWindows) {
		t.Errorf("StopScheduledTask() = %v, want ErrNotWindows", err)
	}
	if _, err := ScheduledTaskStatus("x"); !errors.Is(err, ErrNotWindows) {
		t.Errorf("ScheduledTaskStatus() = %v, want ErrNotWindows", err)
	}
}
