package ui

import (
	"os"
	"path/filepath"
	"testing"

	lytecache "github.com/lytecache/lytecache-go"
)

func TestMergeSourcesDuplicateNameIsHardError(t *testing.T) {
	base := []DBSource{{Name: "a", Path: "/x.db"}, {Name: "a", Path: "/y.db"}}
	_, err := MergeSources(base, nil, nil)
	if err == nil {
		t.Fatal("expected an error for a duplicate configured name, got nil")
	}
}

func TestMergeSourcesScanDiscoversDBFiles(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "svc-a.db"))
	mustCreateFile(t, filepath.Join(dir, "svc-b.db"))
	mustCreateFile(t, filepath.Join(dir, "svc-b.db-wal")) // must NOT be picked up
	mustCreateFile(t, filepath.Join(dir, "readme.txt"))   // must NOT be picked up

	got, err := MergeSources(nil, []string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 discovered sources, got %d: %+v", len(got), got)
	}
	names := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !names["svc-a"] || !names["svc-b"] {
		t.Errorf("expected svc-a and svc-b, got %+v", got)
	}
}

func TestMergeSourcesScanCollisionWarnsAndSkips(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "svc-a.db"))

	var warnings []string
	got, err := MergeSources(
		[]DBSource{{Name: "svc-a", Path: "/explicit.db"}},
		[]string{dir},
		func(msg string) { warnings = append(warnings, msg) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/explicit.db" {
		t.Errorf("expected the explicit source to win, got %+v", got)
	}
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning about the scan collision, got %v", warnings)
	}
}

func TestManagerDegradesMissingFileWithoutCreatingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.db")
	mgr := NewManager([]DBSource{{Name: "missing", Path: path}})
	mgr.WarmUp(func(string, ...any) {})

	e, _ := mgr.entry("missing")
	if _, err := e.Cache("default"); err == nil {
		t.Fatal("expected an error opening a missing database, got nil")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("Manager must never silently create a missing database file")
	}
}

func TestManagerSelfHealsOnceFileAppears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appears-later.db")
	mgr := NewManager([]DBSource{{Name: "later", Path: path}})
	t.Cleanup(func() { _ = mgr.Close() })
	e, _ := mgr.entry("later")

	if _, err := e.Cache("default"); err == nil {
		t.Fatal("expected an error before the file exists")
	}

	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := e.Cache("default"); err != nil {
		t.Fatalf("expected the entry to self-heal once the file exists, got: %v", err)
	}
}

func TestManagerReturnsSameCacheAcrossCalls(t *testing.T) {
	path := tempDBPath(t)
	seed, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager([]DBSource{{Name: "a", Path: path}})
	e, _ := mgr.entry("a")

	c1, err := e.Cache("default")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := e.Cache("default")
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Error("expected the same *Cache instance across calls (open-once-and-hold), got different instances")
	}
	t.Cleanup(func() { _ = mgr.Close() })
}

func TestRescanDiscoversAFileThatAppearedAfterStartup(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "early.db"))

	mgr := NewManager(nil) // nothing configured at "startup" -- --scan hadn't found anything yet
	if got := mgr.Names(); len(got) != 0 {
		t.Fatalf("expected no entries before the first Rescan, got %v", got)
	}

	if err := mgr.Rescan([]string{dir}, nil); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Names(); len(got) != 1 || got[0] != "early" {
		t.Fatalf("expected [early] after the first Rescan, got %v", got)
	}

	// The file a service creates on its first cache write, well after
	// lytecache ui already started (and already ran its one MergeSources
	// pass) -- exactly the case Rescan exists for.
	mustCreateFile(t, filepath.Join(dir, "late.db"))
	if err := mgr.Rescan([]string{dir}, nil); err != nil {
		t.Fatal(err)
	}
	got := mgr.Names()
	if len(got) != 2 {
		t.Fatalf("expected both early and late to be known after the second Rescan, got %v", got)
	}
	names := map[string]bool{got[0]: true, got[1]: true}
	if !names["early"] || !names["late"] {
		t.Errorf("expected early and late, got %v", got)
	}
}

func TestRescanNeverReplacesAnExistingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svc.db")
	mustCreateFile(t, path)

	mgr := NewManager([]DBSource{{Name: "svc", Path: path}})
	before, _ := mgr.entry("svc")

	// Re-scanning the same directory should not disturb the
	// already-registered entry -- same *dbEntry pointer, so any Cache
	// handles it already opened keep working, not silently swapped out.
	if err := mgr.Rescan([]string{dir}, nil); err != nil {
		t.Fatal(err)
	}
	after, ok := mgr.entry("svc")
	if !ok || after != before {
		t.Error("Rescan must never replace an existing entry, even when its path matches exactly")
	}
}

func TestRescanWarnsOnlyForAGenuineCollisionNotAnAlreadyKnownFile(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "svc.db"))

	mgr := NewManager([]DBSource{{Name: "svc", Path: "/explicit/elsewhere.db"}})

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	// First rescan: "svc" from the scan collides with the explicit entry
	// at a genuinely different path -- must warn, matching MergeSources'
	// startup behavior for the same situation.
	if err := mgr.Rescan([]string{dir}, warn); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning for the genuine collision, got %v", warnings)
	}
	if e, _ := mgr.entry("svc"); e.Path != "/explicit/elsewhere.db" {
		t.Error("the explicit entry must not be overwritten by the colliding scan discovery")
	}

	// Second rescan of the exact same, unchanged directory: still colliding
	// with the same explicit entry every time, so this must keep warning
	// (not suppress it just because Rescan has "seen" the scanned file
	// before) -- what must NOT repeat is a warning for a file Rescan
	// itself already registered on a previous pass (see the sibling test).
	if err := mgr.Rescan([]string{dir}, warn); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Errorf("expected a second warning on the second rescan (still colliding), got %v", warnings)
	}
}

func TestRescanStaysQuietForAFileItAlreadyRegisteredItself(t *testing.T) {
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "svc.db"))

	mgr := NewManager(nil)
	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }

	if err := mgr.Rescan([]string{dir}, warn); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("first discovery should never warn, got %v", warnings)
	}

	// Re-scanning the same directory again finds the same file under the
	// same name Rescan itself already registered -- this must NOT be
	// treated as a collision (that would mean a warning every single poll
	// interval, forever, for a file that was never actually a problem).
	if err := mgr.Rescan([]string{dir}, warn); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("re-discovering an already-known file must stay silent, got %v", warnings)
	}
}

func TestRescanWithNoScanDirsIsANoOp(t *testing.T) {
	mgr := NewManager([]DBSource{{Name: "a", Path: "/a.db"}})
	if err := mgr.Rescan(nil, func(string) { t.Error("must not warn") }); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Names(); len(got) != 1 || got[0] != "a" {
		t.Errorf("expected the manager unchanged, got %v", got)
	}
}

func mustCreateFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
