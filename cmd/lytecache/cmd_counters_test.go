package main

import (
	"strings"
	"testing"
)

func TestIncrDecr(t *testing.T) {
	db := tempDBPath(t)

	r := runCLI(t, "", "--db", db, "incr", "counter")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "1" {
		t.Fatalf("incr: code=%d stdout=%q stderr=%s", r.code, r.stdout, r.stderr)
	}

	r = runCLI(t, "", "--db", db, "incr", "counter", "5")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "6" {
		t.Fatalf("incr by 5: code=%d stdout=%q", r.code, r.stdout)
	}

	r = runCLI(t, "", "--db", db, "decr", "counter", "2")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "4" {
		t.Fatalf("decr by 2: code=%d stdout=%q", r.code, r.stdout)
	}

	r = runCLI(t, "", "--db", db, "decr", "counter")
	if r.code != exitSuccess || strings.TrimSpace(r.stdout) != "3" {
		t.Fatalf("decr: code=%d stdout=%q", r.code, r.stdout)
	}
}

func TestIncrInvalidAmount(t *testing.T) {
	db := tempDBPath(t)
	r := runCLI(t, "", "--db", db, "incr", "counter", "not-a-number")
	if r.code != exitUsage {
		t.Fatalf("code = %d, want %d (stderr=%s)", r.code, exitUsage, r.stderr)
	}
}
