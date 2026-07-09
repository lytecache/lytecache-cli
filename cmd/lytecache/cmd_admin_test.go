package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStatsAndInfo(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")
	runCLI(t, "", "--db", db, "get", "k") // one hit
	runCLI(t, "", "--db", db, "get", "missing") // one miss

	for _, cmd := range []string{"stats", "info"} {
		r := runCLI(t, "", "--db", db, cmd)
		if r.code != exitSuccess {
			t.Fatalf("%s: code=%d stderr=%s", cmd, r.code, r.stderr)
		}
		if !strings.Contains(r.stdout, "keys:") || !strings.Contains(r.stdout, "hits:") {
			t.Errorf("%s stdout = %q", cmd, r.stdout)
		}
	}
}

func TestStatsJSON(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "stats", "--json")
	if r.code != exitSuccess {
		t.Fatalf("stats --json: code=%d stderr=%s", r.code, r.stderr)
	}
	var v statsView
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("stats --json output not valid JSON: %v\n%s", err, r.stdout)
	}
	if v.KeyCount != 1 {
		t.Errorf("KeyCount = %d, want 1", v.KeyCount)
	}
	if v.Path != db {
		t.Errorf("Path = %q, want %q", v.Path, db)
	}
}

func TestFlushRequiresConfirmationUnlessYes(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	// Declining leaves the key in place.
	r := runCLI(t, "n\n", "--db", db, "flush")
	if r.code != exitFalseOrMiss {
		t.Fatalf("flush (declined): code=%d, want %d", r.code, exitFalseOrMiss)
	}
	if r := runCLI(t, "", "--db", db, "exists", "k"); r.code != exitSuccess {
		t.Fatalf("key should still exist after declined flush, exists code=%d", r.code)
	}

	// --yes skips the prompt and actually flushes.
	r = runCLI(t, "", "--db", db, "flush", "--yes")
	if r.code != exitSuccess {
		t.Fatalf("flush --yes: code=%d stderr=%s", r.code, r.stderr)
	}
	if r := runCLI(t, "", "--db", db, "exists", "k"); r.code != exitFalseOrMiss {
		t.Fatalf("key should be gone after flush --yes, exists code=%d", r.code)
	}
}

func TestMaintain(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "maintain")
	if r.code != exitSuccess {
		t.Fatalf("maintain: code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "expired") || !strings.Contains(r.stdout, "evicted") {
		t.Errorf("maintain stdout = %q", r.stdout)
	}
}

func TestVacuum(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "vacuum")
	if r.code != exitSuccess {
		t.Fatalf("vacuum: code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "->") {
		t.Errorf("vacuum stdout = %q, want a before -> after size", r.stdout)
	}
}

func TestWhich(t *testing.T) {
	db := tempDBPath(t)

	r := runCLI(t, "", "--db", db, "which")
	if r.code != exitFalseOrMiss {
		t.Fatalf("which (not yet created): code=%d, want %d", r.code, exitFalseOrMiss)
	}
	if !strings.Contains(r.stdout, db) || !strings.Contains(r.stdout, "does not exist") {
		t.Errorf("which stdout = %q", r.stdout)
	}

	runCLI(t, "", "--db", db, "set", "k", "v")
	r = runCLI(t, "", "--db", db, "which")
	if r.code != exitSuccess {
		t.Fatalf("which (created): code=%d, want %d", r.code, exitSuccess)
	}
	if !strings.Contains(r.stdout, "(exists)") {
		t.Errorf("which stdout = %q, want (exists)", r.stdout)
	}
}

func TestWhichRespectsEnvVar(t *testing.T) {
	db := tempDBPath(t)
	t.Setenv("LYTECACHE_PATH", db)

	r := runCLI(t, "", "which")
	if !strings.Contains(r.stdout, db) {
		t.Errorf("which stdout = %q, want it to mention LYTECACHE_PATH's value %q", r.stdout, db)
	}
}

func TestMain(m *testing.M) {
	// LYTECACHE_PATH must never leak from the test process's real
	// environment into a test that doesn't set it itself.
	_ = os.Unsetenv("LYTECACHE_PATH")
	os.Exit(m.Run())
}
