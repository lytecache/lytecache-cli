package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	lytecache "github.com/lytecache/lytecache-go"
)

// doPost logs in first, attaches the session cookie and a valid CSRF
// token, then issues an authenticated POST -- see doGet's identical
// rationale in handlers_test.go.
func doPost(t *testing.T, s *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrfToken := loginTestSession(t, s)
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", csrfToken)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func newDeletableTestServer(t *testing.T, dbs ...DBSource) *Server {
	t.Helper()
	return newTestServerWithConfig(t, Config{Databases: dbs, AllowDelete: true})
}

func TestMutationRoutesReturn403WithoutAllowDelete(t *testing.T) {
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

	s := newTestServer(t, DBSource{Name: "svc", Path: path}) // AllowDelete: false

	cases := []struct {
		path string
		form url.Values
	}{
		{"/db/svc/namespaces/default/delete-key", url.Values{"key": {"k"}}},
		{"/db/svc/namespaces/default/delete-keys", url.Values{"key": {"k"}}},
		{"/db/svc/namespaces/default/flush", url.Values{"confirm_namespace": {"default"}}},
		{"/db/svc/namespaces/default/maintain", nil},
	}
	for _, tc := range cases {
		rec := doPost(t, s, tc.path, tc.form)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: code = %d, want 403", tc.path, rec.Code)
		}
	}

	// The key must still be there -- a 403 must actually mean nothing
	// happened, not just an unchecked response code.
	if found, err := c2(t, path).Exists("k"); err != nil || !found {
		t.Errorf("key was deleted despite a 403, found=%v err=%v", found, err)
	}
}

func TestVacuumIsNotGatedByAllowDelete(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path}) // AllowDelete: false
	rec := doPost(t, s, "/db/svc/namespaces/default/vacuum", nil)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("vacuum without --allow-delete: code = %d, want 303 (vacuum deletes no rows, must not be gated)", rec.Code)
	}
}

func TestDeleteKeyRemovesIt(t *testing.T) {
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

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doPost(t, s, "/db/svc/namespaces/default/delete-key", url.Values{"key": {"k"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}

	if found, err := c2(t, path).Exists("k"); err != nil || found {
		t.Errorf("key still present after delete: found=%v err=%v", found, err)
	}
}

func TestDeleteKeysBulk(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetMany(map[string]any{"a": 1, "b": 2, "c": 3}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doPost(t, s, "/db/svc/namespaces/default/delete-keys", url.Values{"key": {"a", "b"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}

	fresh := c2(t, path)
	for k, wantGone := range map[string]bool{"a": true, "b": true, "c": false} {
		found, err := fresh.Exists(k)
		if err != nil {
			t.Fatal(err)
		}
		if found == wantGone {
			t.Errorf("key %s: found=%v, wantGone=%v", k, found, wantGone)
		}
	}
}

func TestFlushRequiresTypedNamespaceConfirmation(t *testing.T) {
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

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})

	rec := doPost(t, s, "/db/svc/namespaces/default/flush", url.Values{"confirm_namespace": {"wrong"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation: code = %d, want 400", rec.Code)
	}
	if found, err := c2(t, path).Exists("k"); err != nil || !found {
		t.Error("flush must not happen when the confirmation text doesn't match")
	}

	rec = doPost(t, s, "/db/svc/namespaces/default/flush", url.Values{"confirm_namespace": {"default"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("correct confirmation: code = %d, want 303", rec.Code)
	}
	if found, err := c2(t, path).Exists("k"); err != nil || found {
		t.Error("flush should have removed the key")
	}
}

func TestMaintainRunsASweepPass(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path), lytecache.WithSweepInterval(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("stale", "v", lytecache.TTL(0)); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doPost(t, s, "/db/svc/namespaces/default/maintain", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}

	infos, err := c2(t, path).Namespaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 && infos[0].ExpiredPresent != 0 {
		t.Errorf("expected maintain to sweep the expired row, got %+v", infos)
	}
}

// TestNoRouteCanCreateOrModifyAValue is the spec's explicit requirement:
// enumerate every registered route and assert none of them is a
// create/update path, then hit plausible write-shaped paths directly and
// confirm there's nothing there to reach.
func TestNoRouteCanCreateOrModifyAValue(t *testing.T) {
	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: buildFixtureDB(t)})

	// 1. The full registered route table is a known, closed set -- assert
	// it exactly, so any future addition is forced to update this test
	// and justify itself here.
	want := map[string]bool{
		"GET /{$}":                          true,
		"GET /dashboard":                    true,
		"GET /db/{db}":                      true,
		"GET /db/{db}/namespaces/{ns}/keys": true,
		"GET /db/{db}/namespaces/{ns}/keys/{key...}": true,
		"GET /search":           true,
		"GET /healthz":          true,
		"GET /login":            true,
		"POST /login":           true,
		"POST /logout":          true,
		"GET /change-password":  true,
		"POST /change-password": true,
		"POST /db/{db}/namespaces/{ns}/delete-key":  true,
		"POST /db/{db}/namespaces/{ns}/delete-keys": true,
		"POST /db/{db}/namespaces/{ns}/flush":       true,
		"POST /db/{db}/namespaces/{ns}/maintain":    true,
		"POST /db/{db}/namespaces/{ns}/vacuum":      true,
		"GET /static/{path...}":                     true,
		"GET /metrics":                              true,
	}
	got := s.RegisteredRoutes()
	if len(got) != len(want) {
		t.Fatalf("registered %d routes, want exactly %d: %v", len(got), len(want), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected registered route %q -- not in the known-good set", p)
		}
		method := strings.SplitN(p, " ", 2)[0]
		if method == "PUT" || method == "PATCH" {
			t.Errorf("route %q uses %s -- no route may accept a full-value replacement", p, method)
		}
	}

	// 2. Crafted requests to plausible write-shaped paths must find
	// nothing there: 404 (no matching pattern) or 405 (path matches a
	// GET-only pattern, wrong method) -- either way, no handler runs. This
	// is deliberately tested *authenticated*, with a valid CSRF token --
	// the point is that even a legitimately logged-in session, doing
	// everything else right, still has nowhere to send a write. An
	// unauthenticated version of this check would trivially "pass" by
	// hitting the login wall first, proving nothing about routes.
	cookie, csrfToken := loginTestSession(t, s)
	plausibleWritePaths := []struct {
		method, path string
	}{
		{http.MethodPost, "/db/svc/namespaces/default/keys"},              // "create a key at the list URL"
		{http.MethodPut, "/db/svc/namespaces/default/keys/new-key"},       // "PUT to create/replace"
		{http.MethodPatch, "/db/svc/namespaces/default/keys/k-string"},    // "PATCH to edit"
		{http.MethodPost, "/db/svc/namespaces/default/keys/k-string"},     // "POST to the value viewer's own URL"
		{http.MethodPut, "/db/svc/namespaces/default/keys/k-string/ttl"},  // "PUT to extend a TTL"
		{http.MethodPost, "/db/svc/namespaces/default/keys/k-string/set"}, // "a set-like action"
	}
	for _, tc := range plausibleWritePaths {
		body := "value=malicious&csrf_token=" + csrfToken
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: code = %d, want 404 or 405 -- there must be nothing to reach here", tc.method, tc.path, rec.Code)
		}
	}

	// The fixture's values must be provably untouched.
	c := c2(t, s.mgr.entries[0].Path)
	if v, found, err := c.GetString("k-string"); err != nil || !found || v != "hello" {
		t.Errorf("k-string was altered by a crafted write request: v=%q found=%v err=%v", v, found, err)
	}
}

// c2 opens a fresh, independent *lytecache.Cache against path, for
// assertions that must observe on-disk state without going through the
// Server's own cached Cache instance.
func c2(t *testing.T, path string) *lytecache.Cache {
	t.Helper()
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
