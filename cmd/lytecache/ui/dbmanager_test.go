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

func mustCreateFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
