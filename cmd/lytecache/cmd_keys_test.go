package main

import (
	"strings"
	"testing"
)

func TestKeysDefaultPattern(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "user:1", "a")
	runCLI(t, "", "--db", db, "set", "user:2", "b")
	runCLI(t, "", "--db", db, "set", "session:1", "c")

	r := runCLI(t, "", "--db", db, "keys")
	if r.code != exitSuccess {
		t.Fatalf("keys: code=%d stderr=%s", r.code, r.stderr)
	}
	lines := strings.Fields(r.stdout)
	if len(lines) != 3 {
		t.Fatalf("keys stdout = %q, want 3 keys", r.stdout)
	}
}

func TestKeysGlobPattern(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "user:1", "a")
	runCLI(t, "", "--db", db, "set", "user:2", "b")
	runCLI(t, "", "--db", db, "set", "session:1", "c")

	r := runCLI(t, "", "--db", db, "keys", "user:*")
	if r.code != exitSuccess {
		t.Fatalf("keys: code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "user:1") || !strings.Contains(r.stdout, "user:2") ||
		strings.Contains(r.stdout, "session:1") {
		t.Errorf("keys user:* stdout = %q", r.stdout)
	}
}

// scan is a plain alias for keys, kept for Redis muscle memory.
func TestScanAliasesKeys(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "scan")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "k" {
		t.Fatalf("scan: code=%d stdout=%q stderr=%s", r.code, r.stdout, r.stderr)
	}
}

func TestKeysLong(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "42")
	runCLI(t, "", "--db", db, "set", "k2", "v", "--ttl", "300")

	r := runCLI(t, "", "--db", db, "keys", "--long")
	if r.code != exitSuccess {
		t.Fatalf("keys --long: code=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "KEY") || !strings.Contains(r.stdout, "TYPE") ||
		!strings.Contains(r.stdout, "TTL") || !strings.Contains(r.stdout, "SIZE") {
		t.Errorf("keys --long header missing, stdout = %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "int") {
		t.Errorf("keys --long should show int type for k, stdout = %q", r.stdout)
	}
}
