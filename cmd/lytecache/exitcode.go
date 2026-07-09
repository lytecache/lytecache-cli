package main

import "fmt"

// Exit codes. Scripts depend on these -- see the README's "Exit codes"
// section before changing any of them.
const (
	exitSuccess     = 0 // success
	exitFalseOrMiss = 1 // a read found nothing, or a boolean result was false
	exitUsage       = 2 // bad arguments/flags
	exitDatabase    = 3 // the database file/library returned an error
)

// exitError carries a specific exit code out of a command's RunE, letting
// main choose os.Exit's argument without every command duplicating that
// decision. A nil Err means "exit with Code, but there is nothing to print"
// -- used for expected, non-error outcomes like a cache miss, which a
// script can check via the exit code without an "error:" line on stderr.
type exitError struct {
	Code int
	Err  error
}

func (e *exitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *exitError) Unwrap() error { return e.Err }

// silentExit signals exitCode with nothing printed to stderr -- for
// expected outcomes (a miss, a false result) that a script distinguishes
// by exit code alone, not an error message.
func silentExit(code int) error { return &exitError{Code: code} }

// usageErrorf reports a usage error (exit code 2).
func usageErrorf(format string, args ...any) error {
	return &exitError{Code: exitUsage, Err: fmt.Errorf(format, args...)}
}

// databaseError wraps err as a database error (exit code 3).
func databaseError(err error) error {
	if err == nil {
		return nil
	}
	return &exitError{Code: exitDatabase, Err: err}
}
