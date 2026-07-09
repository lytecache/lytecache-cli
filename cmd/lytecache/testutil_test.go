package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// cliResult captures one runWithIO invocation's outcome for assertions.
type cliResult struct {
	stdout string
	stderr string
	code   int
}

// runCLI runs the CLI in-process against args, with stdin as input, without
// touching the real process's stdin/stdout/stderr. This exercises the exact
// same command tree and exit-code unwrapping main() uses (see runWithIO in
// main.go), just against buffers instead of the real streams.
func runCLI(t *testing.T, stdin string, args ...string) cliResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithIO(args, strings.NewReader(stdin), &stdout, &stderr)
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// tempDBPath returns a path to a not-yet-existing database file in a
// per-test temp directory, for tests that need a --db flag.
func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "cache.db")
}
