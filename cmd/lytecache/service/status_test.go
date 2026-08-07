package service

import (
	"os"
	"testing"
	"time"
)

// isolateHome redirects every home-directory-derived path (LogDir, and
// therefore StatePath/installRecordPath/LogPath) into a fresh temp
// directory for the duration of the test -- this package's tests must
// never read or write the real developer machine's actual log/state
// directory.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("LOCALAPPDATA", dir)
}

func TestStateRoundTrips(t *testing.T) {
	isolateHome(t)

	want := State{PID: os.Getpid(), StartedAt: time.Now().Truncate(time.Second), Addr: "127.0.0.1:7070", ConfigPath: "/tmp/ui.yaml"}
	if err := WriteState(want); err != nil {
		t.Fatal(err)
	}

	got, err := ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != want.PID || got.Addr != want.Addr || got.ConfigPath != want.ConfigPath || !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("ReadState() = %+v, want %+v", got, want)
	}

	if err := RemoveState(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(); err == nil {
		t.Error("expected an error reading state after RemoveState")
	}
}

func TestRemoveStateIsIdempotent(t *testing.T) {
	isolateHome(t)
	if err := RemoveState(); err != nil {
		t.Errorf("RemoveState on a nonexistent file should be a no-op, got: %v", err)
	}
}

func TestIsStaleDetectsDeadAndLiveProcesses(t *testing.T) {
	live := State{PID: os.Getpid()}
	if IsStale(live) {
		t.Error("the test's own PID must not be reported as stale")
	}

	// PID 0 is never a real process on any of the three target platforms.
	dead := State{PID: 0}
	if !IsStale(dead) {
		t.Error("PID 0 must be reported as stale")
	}
}

func TestInstallRecordRoundTripsAndDefaultsToOSService(t *testing.T) {
	isolateHome(t)

	// No record written yet -- must default to MethodOSService (covers
	// macOS/Linux, which never write one, and pre-existing installs from
	// before this file existed).
	m, err := ReadInstallRecord()
	if err != nil {
		t.Fatal(err)
	}
	if m != MethodOSService {
		t.Errorf("default method = %q, want %q", m, MethodOSService)
	}

	if err := WriteInstallRecord(MethodScheduledTask); err != nil {
		t.Fatal(err)
	}
	m, err = ReadInstallRecord()
	if err != nil {
		t.Fatal(err)
	}
	if m != MethodScheduledTask {
		t.Errorf("ReadInstallRecord() = %q, want %q", m, MethodScheduledTask)
	}

	if err := RemoveInstallRecord(); err != nil {
		t.Fatal(err)
	}
	m, err = ReadInstallRecord()
	if err != nil {
		t.Fatal(err)
	}
	if m != MethodOSService {
		t.Errorf("after removal, method = %q, want default %q", m, MethodOSService)
	}
}
