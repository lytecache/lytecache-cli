package ui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// auditLogger appends one line per write action (delete/flush/maintain/
// vacuum) to a file next to the config. Deliberately never writes a
// value, only the key name -- see mutations.go's logMutation, the only
// caller.
type auditLogger struct {
	mu sync.Mutex
	f  *os.File
}

// newAuditLogger opens (creating if needed) path for appending, with
// configFileMode -- it can contain key names from a payments system's
// cache, so it gets the same permission discipline as the config file
// itself.
func newAuditLogger(path string) (*auditLogger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, configFileMode)
	if err != nil {
		return nil, fmt.Errorf("opening audit log %s: %w", path, err)
	}
	return &auditLogger{f: f}, nil
}

// log appends one audit line: timestamp, username, remote IP, action,
// database, namespace, key. Never a value.
func (a *auditLogger) log(username, remoteIP, action, db, namespace, key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, _ = fmt.Fprintf(a.f, "%s username=%s remote=%s action=%s db=%s namespace=%s key=%q\n",
		time.Now().UTC().Format(time.RFC3339), username, remoteIP, action, db, namespace, key)
}

func (a *auditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.f.Close()
}
