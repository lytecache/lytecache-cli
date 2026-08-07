package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lytecache "github.com/lytecache/lytecache-go"
)

// These assert the data-* hooks static/*.js depends on are actually
// present in the rendered HTML -- the JS itself isn't executed by these
// Go tests (see docs/ui.md's manual verification steps for that), but a
// silently-renamed/removed attribute here would break it invisibly.

func TestDashboardHasAutoRefreshHooks(t *testing.T) {
	s := newTestServer(t, DBSource{Name: "svc", Path: tempDBPath(t)})
	rec := doGet(t, s, "/dashboard")
	body := rec.Body.String()

	if !strings.Contains(body, `data-refresh-seconds="2"`) {
		t.Error("expected body to carry data-refresh-seconds for static/poll.js")
	}
	if !strings.Contains(body, `id="auto-refresh"`) {
		t.Error("expected the auto-refresh checkbox static/poll.js binds to")
	}
}

func TestKeyBrowserHasKeyboardAndRowHooks(t *testing.T) {
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
	rec := doGet(t, s, "/db/svc/namespaces/default/keys")
	body := rec.Body.String()

	if !strings.Contains(body, `id="key-filter"`) {
		t.Error("expected the glob filter input static/keys.js's \"/\" shortcut focuses")
	}
	if !strings.Contains(body, "data-key-row") {
		t.Error("expected row markers static/keys.js's arrow-key navigation selects")
	}
	// Regression test for a real bug: keysPage embeds both KeyBrowserView
	// and the chrome type Page side by side, and {{.Page}} used to resolve
	// -- silently, via Go's field-promotion rules, no compile error -- to
	// the *embedded chrome struct itself*, not KeyBrowserView's own page
	// number field, dumping the whole struct's Go-syntax representation
	// into the page instead of a page number (see PageNum's doc comment).
	if !strings.Contains(body, "page 1</span>") {
		t.Errorf("expected \"page 1\", got a mangled/incorrect page indicator -- body:\n%s", body)
	}
	if strings.Contains(body, "CSRFToken") || strings.Contains(body, "AllowDelete:") {
		t.Error("page indicator leaked the raw Go struct representation of the chrome/session type")
	}

	if !strings.Contains(body, "data-href=") {
		t.Error("expected data-href on rows for static/keys.js's Enter-to-navigate")
	}
}

func TestValueViewerHasCountdownAndJSONHooks(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("with-ttl", "v", lytecache.TTL(3600e9)); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("json-key", map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})

	rec := doGet(t, s, "/db/svc/namespaces/default/keys/with-ttl")
	if !strings.Contains(rec.Body.String(), "data-expires-at=") {
		t.Error("expected data-expires-at for static/countdown.js on a key with a TTL")
	}

	rec = doGet(t, s, "/db/svc/namespaces/default/keys/json-key")
	if !strings.Contains(rec.Body.String(), `class="json-value" data-json`) {
		t.Error("expected the json-value/data-json hook for static/jsonview.js")
	}
}

func TestStaticAssetsAreServed(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/static/style.css", "/static/keys.js", "/static/countdown.js", "/static/poll.js", "/static/jsonview.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: code = %d, want 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", path)
		}
	}
}

func TestValueViewerHidesDeleteButtonWithoutAllowDelete(t *testing.T) {
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

	readOnly := newTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doGet(t, readOnly, "/db/svc/namespaces/default/keys/k")
	if strings.Contains(rec.Body.String(), "delete key") {
		t.Error("delete control must not be rendered when AllowDelete is false")
	}

	deletable := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	rec = doGet(t, deletable, "/db/svc/namespaces/default/keys/k")
	if !strings.Contains(rec.Body.String(), "delete key") {
		t.Error("expected the delete control to render when AllowDelete is true")
	}
}
