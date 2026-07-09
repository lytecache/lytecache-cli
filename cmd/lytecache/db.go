package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	lytecache "github.com/lytecache/lytecache-go"
)

// expandHome expands a leading "~" or "~/" to the user's home directory.
// This mirrors the library's own (unexported) expandHome -- it is plain
// path handling, not cache logic, and is needed here only so the CLI can
// os.Stat the exact same path New() is about to open, to give a clean
// "database does not exist" message for read-only commands instead of
// letting New() silently create it first.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding ~: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// resolveDBPath returns the path lytecache.New will resolve to, without
// opening anything: dbFlag if set (the --db flag), otherwise
// lytecache.DefaultPath() -- which itself already checks LYTECACHE_PATH
// before falling back to the platform default, so that priority order is
// never duplicated here.
func resolveDBPath(dbFlag string) (string, error) {
	if dbFlag != "" {
		return expandHome(dbFlag)
	}
	return lytecache.DefaultPath()
}

// openCache resolves the database path and opens it, for one-shot mode.
// For a read-only command (readOnly=true), a missing file is reported as a
// clear error instead of being silently created, matching library behavior
// for writes (New always creates a missing file/directory) while giving
// read commands a cleaner failure than "the value doesn't exist because we
// just made an empty database".
//
// In REPL mode (flags.sharedCache is set), this instead returns the
// session's single already-open Cache and a no-op closer -- the REPL owns
// that Cache's lifecycle (opened once at startup, closed on quit), unlike
// one-shot mode's open/act/close-per-command discipline (see the README's
// concurrency note).
func openCache(flags *globalFlags, readOnly bool) (c *lytecache.Cache, closeFn func(), err error) {
	if flags.sharedCache != nil {
		return flags.sharedCache, func() {}, nil
	}

	path, err := resolveDBPath(flags.db)
	if err != nil {
		return nil, nil, databaseError(fmt.Errorf("resolving database path: %w", err))
	}

	if readOnly {
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return nil, nil, databaseError(fmt.Errorf("database file does not exist: %s", path))
			}
			return nil, nil, databaseError(fmt.Errorf("checking database file %s: %w", path, err))
		}
	}

	opts := []lytecache.Option{lytecache.WithPath(path)}
	if flags.namespace != "" {
		opts = append(opts, lytecache.WithNamespace(flags.namespace))
	}

	c, err = lytecache.New(opts...)
	if err != nil {
		return nil, nil, databaseError(err)
	}
	return c, func() { _ = c.Close() }, nil
}
