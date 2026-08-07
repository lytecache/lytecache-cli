package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	lytecache "github.com/lytecache/lytecache-go"
)

// DBSource names one configured database file, from a config file's
// `databases:` list or a repeatable --db name=/path/to.db flag.
type DBSource struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`

	// MaxKeys/MaxBytes are optional, operator-declared capacity hints for
	// the dashboard's usage-against-cap display. They are informational
	// only -- lytecache.New's WithMaxKeys/WithMaxBytes options configure
	// eviction behavior for one process's *Cache and are never persisted
	// to the database file itself, so a Cache this UI opens has no way to
	// discover what limits the *application's* process configured for the
	// same file. An operator who wants usage bars declares the same caps
	// here, in the config file's `databases:` list; left nil, the
	// dashboard just shows "unlimited" for that database, per the spec's
	// "usage bars where those limits are known".
	MaxKeys  *int64 `yaml:"max_keys,omitempty"`
	MaxBytes *int64 `yaml:"max_bytes,omitempty"`
}

// MergeSources combines explicitly configured sources (config file +
// --db flags, in that order -- the caller is responsible for that
// ordering) with auto-discovered *.db files under scanDirs, into one
// ordered list. A duplicate name among the explicit sources is a hard
// error, since silently overwriting a named entry would be a real
// surprise for an operator. A scan-discovered name colliding with an
// already-configured one is a lower-stakes, auto-derived conflict --
// it is skipped and reported via warn instead of failing startup.
func MergeSources(base []DBSource, scanDirs []string, warn func(string)) ([]DBSource, error) {
	seen := make(map[string]string, len(base))
	out := make([]DBSource, 0, len(base))
	for _, s := range base {
		if existing, ok := seen[s.Name]; ok {
			return nil, fmt.Errorf("duplicate database name %q (%s and %s) -- give one of them a distinct name", s.Name, existing, s.Path)
		}
		seen[s.Name] = s.Path
		out = append(out, s)
	}

	for _, dir := range scanDirs {
		found, err := scanDBFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, s := range found {
			if existing, ok := seen[s.Name]; ok {
				if warn != nil {
					warn(fmt.Sprintf("--scan %s: %q already configured (%s), skipping %s", dir, s.Name, existing, s.Path))
				}
				continue
			}
			seen[s.Name] = s.Path
			out = append(out, s)
		}
	}
	return out, nil
}

// scanDBFiles globs dir non-recursively for *.db files, deriving each
// one's Name from its filename (without the .db suffix) -- the shared
// core both MergeSources (startup) and Manager.Rescan (periodic
// re-discovery, see its doc comment) glob with, so the two can never
// drift in how they derive a name from a path.
func scanDBFiles(dir string) ([]DBSource, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.db"))
	if err != nil {
		return nil, fmt.Errorf("--scan %s: %w", dir, err)
	}
	sort.Strings(matches)
	out := make([]DBSource, len(matches))
	for i, m := range matches {
		out[i] = DBSource{Name: strings.TrimSuffix(filepath.Base(m), ".db"), Path: m}
	}
	return out, nil
}

// dbEntry holds every *lytecache.Cache opened against one configured
// database file, one per namespace visited so far. Caches are opened once
// and held for the server's lifetime (never opened/closed per request),
// mirroring the discipline the CLI's own REPL mode already uses for its
// single shared Cache (see cmd/lytecache/db.go's openCache).
type dbEntry struct {
	Name string
	Path string

	// DeclaredMaxKeys/DeclaredMaxBytes are the operator-declared capacity
	// hints from DBSource -- see its doc comment for why these can't be
	// discovered from the file itself.
	DeclaredMaxKeys  *int64
	DeclaredMaxBytes *int64

	mu     sync.Mutex
	caches map[string]*lytecache.Cache
}

// Cache returns the Cache for this database's namespace, opening (and
// caching) it on first use. A missing/locked/incompatible-schema file
// returns an error every time rather than being remembered permanently --
// so a file that starts out unreachable and later becomes reachable is
// picked up on the very next call, with no server restart required.
//
// Unlike a bare lytecache.New, this never silently creates a missing file
// -- it stats the path first, matching the CLI's own read-only-command
// discipline (see openCache's readOnly branch), since this is an
// inspection tool, not something that should conjure new cache files into
// existence just because someone browsed to a typo'd database name.
func (e *dbEntry) Cache(namespace string) (*lytecache.Cache, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if c, ok := e.caches[namespace]; ok {
		return c, nil
	}
	if _, err := os.Stat(e.Path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("database file does not exist: %s", e.Path)
		}
		return nil, fmt.Errorf("database file %s: %w", e.Path, err)
	}
	c, err := lytecache.New(lytecache.WithPath(e.Path), lytecache.WithNamespace(namespace))
	if err != nil {
		return nil, err
	}
	e.caches[namespace] = c
	return c, nil
}

// Manager holds every configured database, in configured order. entries/
// byName were immutable-after-construction until Rescan (see its doc
// comment) -- mu protects them now that they aren't.
type Manager struct {
	mu      sync.RWMutex
	entries []*dbEntry
	byName  map[string]*dbEntry
}

// NewManager builds a Manager from an already-merged, name-deduplicated
// source list (see MergeSources). It does not open anything yet -- call
// WarmUp to eagerly open each entry's default namespace.
func NewManager(sources []DBSource) *Manager {
	m := &Manager{byName: make(map[string]*dbEntry, len(sources))}
	for _, s := range sources {
		m.addLocked(s)
	}
	return m
}

// addLocked appends a new entry for s. Caller must hold mu for writing.
func (m *Manager) addLocked(s DBSource) *dbEntry {
	e := &dbEntry{
		Name:             s.Name,
		Path:             s.Path,
		DeclaredMaxKeys:  s.MaxKeys,
		DeclaredMaxBytes: s.MaxBytes,
		caches:           make(map[string]*lytecache.Cache),
	}
	m.entries = append(m.entries, e)
	m.byName[s.Name] = e
	return e
}

// Rescan re-globs scanDirs (the same directories --scan was given at
// startup) and adds a new entry for any *.db file that has appeared since
// the last scan -- MergeSources (and therefore NewManager) only ever runs
// once, at server startup, so without this, a service whose first cache
// write happens after lytecache ui has already started would stay
// invisible until the process is restarted, no matter how much data it
// writes afterward. Existing entries (whether from --db or an earlier
// scan) are left completely alone -- their already-open Cache handles
// keep working exactly as before; Rescan only ever adds, never replaces
// or removes.
//
// A newly-scanned name colliding with an existing entry pointing at a
// *different* path is a genuine naming collision, reported via warn
// exactly like MergeSources does at startup. A name colliding with an
// existing entry at the *same* path is not a collision -- it's the same
// file Rescan already knows about from a previous pass -- and is skipped
// silently, since warning about it every poll interval forever would be
// pure log spam for a file that was never actually a problem.
func (m *Manager) Rescan(scanDirs []string, warn func(string)) error {
	if len(scanDirs) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, dir := range scanDirs {
		found, err := scanDBFiles(dir)
		if err != nil {
			return err
		}
		for _, s := range found {
			existing, ok := m.byName[s.Name]
			switch {
			case !ok:
				m.addLocked(s)
			case existing.Path != s.Path && warn != nil:
				warn(fmt.Sprintf("--scan %s: %q already configured (%s), skipping %s", dir, s.Name, existing.Path, s.Path))
			}
		}
	}
	return nil
}

// WarmUp eagerly opens the default namespace for every configured
// database, so the first dashboard render doesn't pay that latency and so
// an unreachable file is visible in the startup log right away. It never
// fails the whole server -- a startup failure for one entry just means
// that entry's Cache call will keep failing (and self-healing, per
// Cache's own doc comment) until whatever's wrong is fixed.
func (m *Manager) WarmUp(logf func(format string, args ...any)) {
	for _, e := range m.entries {
		if _, err := e.Cache("default"); err != nil && logf != nil {
			logf("lytecache ui: database %q not yet reachable: %v", e.Name, err)
		}
	}
}

// Names returns configured database names, in configured order.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, len(m.entries))
	for i, e := range m.entries {
		names[i] = e.Name
	}
	return names
}

// entry looks up a configured database by name. Unexported (and returning
// the unexported *dbEntry) since every caller is inside this package --
// handlers.go/pages.go/metrics.go/viewmodel.go all call it directly, never
// from package main.
func (m *Manager) entry(name string) (*dbEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byName[name]
	return e, ok
}

// Close closes every Cache this Manager has opened.
func (m *Manager) Close() error {
	var errs []error
	for _, e := range m.entries {
		e.mu.Lock()
		for _, c := range e.caches {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		e.mu.Unlock()
	}
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, err := range errs {
		msgs[i] = err.Error()
	}
	return fmt.Errorf("closing databases: %s", strings.Join(msgs, "; "))
}
