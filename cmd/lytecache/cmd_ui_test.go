package main

import (
	"reflect"
	"testing"

	"github.com/lytecache/lytecache-cli/cmd/lytecache/ui"
)

func TestDefaultScanDirsIfUnconfigured(t *testing.T) {
	const cacheDir = "/home/x/.cache"
	want := []string{"/home/x/.cache/lytecache", dockerSharedCacheDir}

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
