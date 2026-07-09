package main

import (
	"strings"
	"testing"
)

func TestTTLNoExpiry(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "ttl", "k")
	if r.code != exitSuccess {
		t.Fatalf("ttl: code=%d stderr=%s", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "-1" {
		t.Errorf("stdout = %q, want -1", r.stdout)
	}
}

func TestTTLMissingKey(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "other", "v")

	r := runCLI(t, "", "--db", db, "ttl", "missing")
	if r.code != exitFalseOrMiss {
		t.Fatalf("code = %d, want %d", r.code, exitFalseOrMiss)
	}
	if r.stdout != "(nil)\n" {
		t.Errorf("stdout = %q, want (nil)", r.stdout)
	}
}

func TestExpirePersistTouch(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")

	r := runCLI(t, "", "--db", db, "expire", "k", "300")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "1" {
		t.Fatalf("expire: code=%d stdout=%q", r.code, r.stdout)
	}

	r = runCLI(t, "", "--db", db, "ttl", "k")
	if strings.TrimSpace(r.stdout) == "-1" {
		t.Errorf("ttl after expire should not be -1, got %q", r.stdout)
	}

	r = runCLI(t, "", "--db", db, "persist", "k")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "1" {
		t.Fatalf("persist: code=%d stdout=%q", r.code, r.stdout)
	}
	r = runCLI(t, "", "--db", db, "ttl", "k")
	if strings.TrimSpace(r.stdout) != "-1" {
		t.Errorf("ttl after persist = %q, want -1", r.stdout)
	}

	r = runCLI(t, "", "--db", db, "touch", "k", "60")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "1" {
		t.Fatalf("touch: code=%d stdout=%q", r.code, r.stdout)
	}

	r = runCLI(t, "", "--db", db, "expire", "missing", "60")
	if r.code != exitFalseOrMiss || strings.TrimSpace(r.stdout) != "0" {
		t.Errorf("expire missing: code=%d stdout=%q", r.code, r.stdout)
	}
}

func TestExpireInvalidSeconds(t *testing.T) {
	db := tempDBPath(t)
	runCLI(t, "", "--db", db, "set", "k", "v")
	r := runCLI(t, "", "--db", db, "expire", "k", "not-a-number")
	if r.code != exitUsage {
		t.Fatalf("code = %d, want %d (stderr=%s)", r.code, exitUsage, r.stderr)
	}
}
