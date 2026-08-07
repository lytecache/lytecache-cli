package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lytecache/lytecache-cli/cmd/lytecache/ui"
)

func TestDefaultScanDirsIfUnconfigured(t *testing.T) {
	// Built via filepath.Join, not a hand-written literal -- Join uses the
	// native separator for whatever cacheDir it's given (backslash on
	// Windows), so a forward-slash literal here would mismatch the
	// function under test's real output on that platform.
	cacheDir := filepath.Join(string(filepath.Separator)+"home", "x", ".cache")
	want := []string{filepath.Join(cacheDir, "lytecache"), dockerSharedCacheDir}

	got := defaultScanDirsIfUnconfigured(nil, nil, cacheDir)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zero config: got %v, want %v", got, want)
	}

	// An explicit --db entry must fully override the default, not
	// combine with it.
	dbs := []ui.DBSource{{Name: "a", Path: "/a.db"}}
	if got := defaultScanDirsIfUnconfigured(dbs, nil, cacheDir); got != nil {
		t.Errorf("with --db configured: got %v, want nil (no default scan)", got)
	}

	// An explicit --scan entry must also fully override the default.
	explicit := []string{"/somewhere/else"}
	got = defaultScanDirsIfUnconfigured(nil, explicit, cacheDir)
	if !reflect.DeepEqual(got, explicit) {
		t.Errorf("with --scan configured: got %v, want %v (unchanged)", got, explicit)
	}
}
