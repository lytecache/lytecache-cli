package main

import (
	"strings"
	"testing"
)

func TestCheckNotRootOnGOOS(t *testing.T) {
	if err := checkNotRootOnGOOS("darwin", 0, "lytecache service start"); err == nil {
		t.Error("expected an error for root on darwin")
	} else if got := err.Error(); !strings.Contains(got, "sudo") || !strings.Contains(got, "LaunchAgent") {
		t.Errorf("error should mention sudo and LaunchAgent, got: %v", got)
	}

	if err := checkNotRootOnGOOS("darwin", 501, "lytecache service start"); err != nil {
		t.Errorf("expected no error for a normal user on darwin, got: %v", err)
	}

	// Linux legitimately needs root for `service install --system` (and
	// therefore for start/stop/etc. against that install too) -- this
	// check must never fire there.
	if err := checkNotRootOnGOOS("linux", 0, "lytecache service start"); err != nil {
		t.Errorf("expected no error for root on linux (--system is a real, supported mode), got: %v", err)
	}

	// os.Geteuid() always returns -1 on Windows; must never fire there
	// either.
	if err := checkNotRootOnGOOS("windows", -1, "lytecache service start"); err != nil {
		t.Errorf("expected no error on windows, got: %v", err)
	}
}
