package ui

import (
	"strings"
	"testing"
	"time"

	lytecache "github.com/lytecache/lytecache-go"
)

func TestBuildDashboardHealthyAndUnhealthy(t *testing.T) {
	healthyPath := tempDBPath(t)
	hc, err := lytecache.New(lytecache.WithPath(healthyPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := hc.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := hc.Close(); err != nil {
		t.Fatal(err)
	}

	// The application process that owns overCapPath configures MaxKeys(10)
	// for its own *Cache -- but that's an in-memory Option, never
	// persisted to the file (see DBSource's doc comment), so a UI-opened
	// Cache against the same file has no way to rediscover it. The
	// operator declares the same cap on the DBSource below purely for the
	// dashboard's display/health purposes; the UI's own Cache is never
	// opened with WithMaxKeys itself, so it never enforces eviction on a
	// file another process owns.
	overCapPath := tempDBPath(t)
	oc, err := lytecache.New(lytecache.WithPath(overCapPath), lytecache.WithMaxKeys(10), lytecache.WithEviction(lytecache.NoEviction))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		if err := oc.Set(string(rune('a'+i)), "v"); err != nil {
			t.Fatal(err)
		}
	}
	if err := oc.Close(); err != nil {
		t.Fatal(err)
	}
	declaredMaxKeys := int64(10)

	mgr := NewManager([]DBSource{
		{Name: "healthy", Path: healthyPath},
		{Name: "overcap", Path: overCapPath, MaxKeys: &declaredMaxKeys},
		{Name: "unreadable", Path: tempDBPath(t) + "-does-not-exist"},
	})
	mgr.WarmUp(func(string, ...any) {})
	t.Cleanup(func() { _ = mgr.Close() })

	rows := BuildDashboard(mgr)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	byName := make(map[string]DashboardRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}

	if byName["healthy"].Err != "" || byName["healthy"].Unhealthy {
		t.Errorf("healthy row should be healthy, got %+v", byName["healthy"])
	}
	if byName["healthy"].KeyCount != 1 {
		t.Errorf("healthy KeyCount = %d, want 1", byName["healthy"].KeyCount)
	}

	if !byName["overcap"].Unhealthy {
		t.Errorf("overcap row (9/10 = 90%% of max_keys) should be flagged unhealthy, got %+v", byName["overcap"])
	}

	if byName["unreadable"].Err == "" {
		t.Errorf("unreadable row should carry an error, got %+v", byName["unreadable"])
	}
}

func TestBuildDashboardEmptyConfiguration(t *testing.T) {
	mgr := NewManager(nil)
	rows := BuildDashboard(mgr)
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for an empty configuration, got %d", len(rows))
	}
}

func TestBuildKeyBrowserGlobSortPaginate(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.SetMany(map[string]any{
		"session:3": "ccc",
		"session:1": "a",
		"session:2": "bb",
		"user:1":    "x",
	}); err != nil {
		t.Fatal(err)
	}

	view, err := BuildKeyBrowser(c, "db", "default", "session:*", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if view.TotalCount != 3 {
		t.Fatalf("expected 3 matches for session:*, got %d", view.TotalCount)
	}
	if view.Rows[0].Key != "session:1" || view.Rows[2].Key != "session:3" {
		t.Errorf("expected default ascending key sort, got %+v", view.Rows)
	}

	descBySize, err := BuildKeyBrowser(c, "db", "default", "session:*", "-size", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if descBySize.Rows[0].Key != "session:3" {
		t.Errorf("expected session:3 (largest value) first when sorting -size, got %+v", descBySize.Rows)
	}

	page1, err := BuildKeyBrowser(c, "db", "default", "session:*", "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Rows) != 2 {
		t.Fatalf("page size 2: expected 2 rows on page 1, got %d", len(page1.Rows))
	}
	page2, err := BuildKeyBrowser(c, "db", "default", "session:*", "", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Rows) != 1 {
		t.Fatalf("page size 2: expected 1 row on page 2, got %d", len(page2.Rows))
	}
}

func TestBuildValueViewEveryTypeCode(t *testing.T) {
	path := buildFixtureDB(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	cases := []struct {
		key         string
		wantSubstr  string
		nonPortable bool
	}{
		{"k-bytes", "(3 bytes, base64)", false},
		{"k-string", "hello", false},
		{"k-int", "42", false},
		{"k-float", "3.14", false},
		{"k-json", `"a": 1`, false},
		{"k-python", "written by another language", true},
		{"k-java", "written by another language", true},
	}
	for _, tc := range cases {
		v, err := BuildValueView(c, "db", "default", tc.key, nil)
		if err != nil {
			t.Errorf("%s: %v", tc.key, err)
			continue
		}
		if !v.Found {
			t.Errorf("%s: expected Found=true", tc.key)
			continue
		}
		if v.NonPortable != tc.nonPortable {
			t.Errorf("%s: NonPortable = %v, want %v", tc.key, v.NonPortable, tc.nonPortable)
		}
		got := v.Rendered
		if tc.nonPortable {
			got = v.Message
		}
		if !strings.Contains(got, tc.wantSubstr) {
			t.Errorf("%s: rendered = %q, want it to contain %q", tc.key, got, tc.wantSubstr)
		}
	}
}

func TestBuildValueViewMasking(t *testing.T) {
	c, err := lytecache.New(lytecache.WithPath(tempDBPath(t)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Set("session:otp:1", "123456"); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("session:other", "not sensitive"); err != nil {
		t.Fatal(err)
	}

	masked, err := BuildValueView(c, "db", "default", "session:otp:1", []string{"*otp*"})
	if err != nil {
		t.Fatal(err)
	}
	if !masked.Masked {
		t.Error("expected session:otp:1 to be masked")
	}
	if strings.Contains(masked.Rendered, "123456") || strings.Contains(masked.Message, "123456") {
		t.Errorf("masked value must never contain the real value, got Rendered=%q Message=%q", masked.Rendered, masked.Message)
	}

	unmasked, err := BuildValueView(c, "db", "default", "session:other", []string{"*otp*"})
	if err != nil {
		t.Fatal(err)
	}
	if unmasked.Masked {
		t.Error("session:other should not match *otp* and must not be masked")
	}
	if unmasked.Rendered != "not sensitive" {
		t.Errorf("unmasked Rendered = %q, want the real value", unmasked.Rendered)
	}
}

func TestBuildValueViewMissingKey(t *testing.T) {
	c, err := lytecache.New(lytecache.WithPath(tempDBPath(t)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	v, err := BuildValueView(c, "db", "default", "nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Found {
		t.Error("expected Found=false for a missing key")
	}
}

func TestBuildSearchAcrossNamespaces(t *testing.T) {
	path := tempDBPath(t)
	a, err := lytecache.New(lytecache.WithPath(path), lytecache.WithNamespace("ns-a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Set("shared:1", "v"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := lytecache.New(lytecache.WithPath(path), lytecache.WithNamespace("ns-b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set("shared:2", "v"); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager([]DBSource{{Name: "db1", Path: path}})
	mgr.WarmUp(func(string, ...any) {})
	t.Cleanup(func() { _ = mgr.Close() })

	view := BuildSearch(mgr, "shared:*")
	if len(view.Rows) != 2 {
		t.Fatalf("expected 2 matches across both namespaces, got %d: %+v", len(view.Rows), view.Rows)
	}
	seen := map[string]bool{}
	for _, r := range view.Rows {
		seen[r.Namespace+"/"+r.Key] = true
	}
	if !seen["ns-a/shared:1"] || !seen["ns-b/shared:2"] {
		t.Errorf("expected results from both namespaces, got %+v", view.Rows)
	}
}

func TestKeyRowTTLDisplay(t *testing.T) {
	d := 90 * time.Second
	r := KeyRow{TTLRemaining: &d}
	if got := r.TTLDisplay(); got != "1m30s" {
		t.Errorf("TTLDisplay() = %q, want 1m30s", got)
	}
	if got := (KeyRow{}).TTLDisplay(); got != "-" {
		t.Errorf("TTLDisplay() with no TTL = %q, want -", got)
	}
}
