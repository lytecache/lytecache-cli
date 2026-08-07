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
		matches, err := filepath.Glob(filepath.Join(dir, "*.db"))
		if err != nil {
			return nil, fmt.Errorf("--scan %s: %w", dir, err)
		}
		sort.Strings(matches)
		for _, m := range matches {
			name := strings.TrimSuffix(filepath.Base(m), ".db")
			if existing, ok := seen[name]; ok {
				if warn != nil {
					warn(fmt.Sprintf("--scan %s: %q already configured (%s), skipping %s", dir, name, existing, m))
				}
				continue
			}
			seen[name] = m
			out = append(out, DBSource{Name: name, Path: m})
		}
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

// Manager holds every configured database, in configured order.
type Manager struct {
	entries []*dbEntry
	byName  map[string]*dbEntry
}

// NewManager builds a Manager from an already-merged, name-deduplicated
// source list (see MergeSources). It does not open anything yet -- call
// WarmUp to eagerly open each entry's default namespace.
func NewManager(sources []DBSource) *Manager {
	m := &Manager{byName: make(map[string]*dbEntry, len(sources))}
	for _, s := range sources {
		e := &dbEntry{
			Name:             s.Name,
			Path:             s.Path,
			DeclaredMaxKeys:  s.MaxKeys,
			DeclaredMaxBytes: s.MaxBytes,
			caches:           make(map[string]*lytecache.Cache),
		}
		m.entries = append(m.entries, e)
		m.byName[s.Name] = e
	}
	return m
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
	names := make([]string, len(m.entries))
	for i, e := range m.entries {
		names[i] = e.Name
	}
	return names
}

// Entry looks up a configured database by name.
func (m *Manager) Entry(name string) (*dbEntry, bool) {
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
