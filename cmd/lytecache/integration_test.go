package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestScriptStyleIntegration compiles the real lytecache binary and drives
// it exactly the way a shell script would (per the spec's "script-friendly"
// one-shot mode) -- set -> get -> incr -> ttl -> flush --yes -- to catch
// anything that only breaks through the compiled binary + os.Exit path
// (as opposed to the in-process runWithIO used by every other test here).
func TestScriptStyleIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compiled-binary integration test in -short mode")
	}

	binPath := filepath.Join(t.TempDir(), "lytecache-test-bin")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	db := filepath.Join(t.TempDir(), "cache.db")
	run := func(args ...string) (stdout string, code int) {
		t.Helper()
		fullArgs := append([]string{"--db", db}, args...)
		cmd := exec.Command(binPath, fullArgs...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &bytes.Buffer{}
		err := cmd.Run()
		if err == nil {
			return out.String(), 0
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return out.String(), exitErr.ExitCode()
		}
		t.Fatalf("running %v: %v", fullArgs, err)
		return "", -1
	}

	if _, code := run("set", "user:1", `{"name":"Ada"}`); code != exitSuccess {
		t.Fatalf("set: exit code %d, want %d", code, exitSuccess)
	}

	stdout, code := run("get", "user:1")
	if code != exitSuccess {
		t.Fatalf("get: exit code %d, want %d", code, exitSuccess)
	}
	if !strings.Contains(stdout, `"name": "Ada"`) {
		t.Errorf("get stdout = %q, want it to contain the Ada value", stdout)
	}

	if _, code := run("set", "counter", "10"); code != exitSuccess {
		t.Fatalf("set counter: exit code %d", code)
	}
	stdout, code = run("incr", "counter", "5")
	if code != exitSuccess || strings.TrimSpace(stdout) != "15" {
		t.Fatalf("incr: exit code %d, stdout %q, want 15", code, stdout)
	}

	if _, code := run("set", "temp", "v", "--ttl", "300"); code != exitSuccess {
		t.Fatalf("set temp: exit code %d", code)
	}
	stdout, code = run("ttl", "temp")
	if code != exitSuccess {
		t.Fatalf("ttl: exit code %d", code)
	}
	if strings.TrimSpace(stdout) == "-1" {
		t.Errorf("ttl stdout = %q, want a positive remaining-seconds value", stdout)
	}

	if _, code := run("flush", "--yes"); code != exitSuccess {
		t.Fatalf("flush --yes: exit code %d", code)
	}
	if _, code := run("exists", "user:1"); code != exitFalseOrMiss {
		t.Fatalf("exists after flush: exit code %d, want %d", code, exitFalseOrMiss)
	}

	// A missing key in one-shot mode is exit 1 with "(nil)", not an error.
	stdout, code = run("get", "user:1")
	if code != exitFalseOrMiss || strings.TrimSpace(stdout) != "(nil)" {
		t.Errorf("get after flush: code=%d stdout=%q", code, stdout)
	}
}
