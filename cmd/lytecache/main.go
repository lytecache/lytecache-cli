// Command lytecache is a command-line tool for interacting with lytecache
// database files, reusing the core library for every cache operation.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the CLI against the real process streams and returns the
// process exit code, rather than calling os.Exit directly, so it can be
// exercised from tests.
func run(args []string) int {
	return runWithIO(args, os.Stdin, os.Stdout, os.Stderr)
}

// runWithIO is run's implementation, parameterized over stdin/stdout/stderr
// so command-level tests can capture output without touching the real
// process streams or shelling out to a compiled binary.
func runWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.Err != nil {
				_, _ = fmt.Fprintln(stderr, "error:", ee.Err)
			}
			return ee.Code
		}
		// A plain error reaching here is cobra's own usage validation (bad
		// flag, wrong arg count) -- always a usage error.
		_, _ = fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	return exitSuccess
}
