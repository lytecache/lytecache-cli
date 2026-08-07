package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lytecache "github.com/lytecache/lytecache-go"
)

// doGet logs in first, then issues an authenticated GET -- these tests
// exercise routing/data logic behind the auth middleware, not auth itself
// (see auth_test.go for unauthenticated-access, CSRF, and rate-limit
// coverage).
func doGet(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	cookie, _ := loginTestSession(t, s)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestIndexRedirectsToDashboard(t *testing.T) {
	s := newTestServer(t)
	rec := doGet(t, s, "/")
	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("Location = %q, want /dashboard", loc)
	}
}

func TestDashboardWithNoConfiguredDatabasesExplainsHowToConfigure(t *testing.T) {
	s := newTestServer(t)
	rec := doGet(t, s, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "--db") || !strings.Contains(body, "--scan") {
		t.Errorf("expected the empty-config message to mention --db/--scan, got:\n%s", body)
	}
	if strings.Contains(strings.ToLower(body), "add database") {
		t.Error("must never offer an \"add database\" UI action")
	}
}

func TestDashboardListsConfiguredDatabase(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doGet(t, s, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "svc") {
		t.Errorf("expected the dashboard to list \"svc\", got:\n%s", rec.Body.String())
	}
}

func TestUnknownDatabaseIs404(t *testing.T) {
	s := newTestServer(t)
	rec := doGet(t, s, "/db/nope/namespaces/default/keys")
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

func TestMissingDatabaseFileDegradesGracefullyNotA500(t *testing.T) {
	path := tempDBPath(t) // never created
	s := newTestServer(t, DBSource{Name: "gone", Path: path})

	rec := doGet(t, s, "/db/gone/namespaces/default/keys")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (inline error, not a hard failure)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not reachable") {
		t.Errorf("expected an inline reachability error, got:\n%s", rec.Body.String())
	}
}

func TestNamespaceIsolationBetweenTwoDatabasesWithOverlappingKeys(t *testing.T) {
	pathA := tempDBPath(t)
	a, err := lytecache.New(lytecache.WithPath(pathA))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Set("shared-key", "value-from-a"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	pathB := tempDBPath(t)
	b, err := lytecache.New(lytecache.WithPath(pathB))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set("shared-key", "value-from-b"); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "a", Path: pathA}, DBSource{Name: "b", Path: pathB})

	recA := doGet(t, s, "/db/a/namespaces/default/keys/shared-key")
	if !strings.Contains(recA.Body.String(), "value-from-a") {
		t.Errorf("db a: expected value-from-a, got:\n%s", recA.Body.String())
	}
	if strings.Contains(recA.Body.String(), "value-from-b") {
		t.Errorf("db a: leaked db b's value:\n%s", recA.Body.String())
	}

	recB := doGet(t, s, "/db/b/namespaces/default/keys/shared-key")
	if !strings.Contains(recB.Body.String(), "value-from-b") {
		t.Errorf("db b: expected value-from-b, got:\n%s", recB.Body.String())
	}
	if strings.Contains(recB.Body.String(), "value-from-a") {
		t.Errorf("db b: leaked db a's value:\n%s", recB.Body.String())
	}
}

func TestKeyDetailNonPortableValueMessage(t *testing.T) {
	path := buildFixtureDB(t)
	s := newTestServer(t, DBSource{Name: "fx", Path: path})

	rec := doGet(t, s, "/db/fx/namespaces/default/keys/k-python")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "written by another language") {
		t.Errorf("expected the non-portable message, got:\n%s", rec.Body.String())
	}
}

func TestKeyDetailWithSlashInKey(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("a/b/c", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doGet(t, s, "/db/svc/namespaces/default/keys/a/b/c")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 -- {key...} must capture slashes", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "a/b/c") {
		t.Errorf("expected the rendered page to echo the full slash-containing key, got:\n%s", rec.Body.String())
	}
}

func TestSearchAcrossDatabases(t *testing.T) {
	pathA := tempDBPath(t)
	a, err := lytecache.New(lytecache.WithPath(pathA))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Set("order:1", "v"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "orders", Path: pathA})
	rec := doGet(t, s, "/search?q=order:*")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "order:1") {
		t.Errorf("expected search results to include order:1, got:\n%s", rec.Body.String())
	}
}

func TestHealthzUnconditionallyOK(t *testing.T) {
	s := newTestServer(t)
	rec := doGet(t, s, "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", rec.Code)
	}
}

func TestDBRedirectGoesToDefaultNamespace(t *testing.T) {
	s := newTestServer(t, DBSource{Name: "svc", Path: tempDBPath(t)})
	rec := doGet(t, s, "/db/svc")
	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/db/svc/namespaces/default/keys" {
		t.Errorf("Location = %q, want /db/svc/namespaces/default/keys", loc)
	}
}
