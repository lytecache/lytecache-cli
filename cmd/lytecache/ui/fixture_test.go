package ui

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lytecache "github.com/lytecache/lytecache-go"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "cache.db")
}

// newTestCacheAt opens a *lytecache.Cache at an explicit path (creating it
// if needed), for tests that seed a fixture file before pointing a Server
// at it -- as opposed to newTestCache-style helpers elsewhere that also
// pick the path.
func newTestCacheAt(path string) (*lytecache.Cache, error) {
	return lytecache.New(lytecache.WithPath(path))
}

// buildFixtureDB creates a database file via the real library, then
// inserts one row per value_type code (0-6) directly via raw SQL -- codes
// 5/6 (python-pickle/java-serialized) can only be produced this way, since
// the library itself never writes them. Mirrors
// cmd/lytecache/fixture_test.go's identical helper.
func buildFixtureDB(t *testing.T) string {
	t.Helper()
	path := tempDBPath(t)

	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UnixMilli()
	const insert = `
INSERT INTO cache (key, namespace, value, value_type, created_at, expires_at, last_accessed, access_count, size_bytes)
VALUES (?, 'default', ?, ?, ?, NULL, ?, 0, ?)`

	rows := []struct {
		key   string
		value []byte
		code  int
	}{
		{"k-bytes", []byte{0x01, 0x02, 0x03}, typeBytes},
		{"k-string", []byte("hello"), typeString},
		{"k-int", []byte("42"), typeInt},
		{"k-float", []byte("3.14"), typeFloat},
		{"k-json", []byte(`{"a":1}`), typeJSON},
		{"k-python", []byte{0x80, 0x04, 0x95, 0x00, 0x01}, typePython},
		{"k-java", []byte{0xac, 0xed, 0x00, 0x05, 0x77}, typeJava},
	}
	for _, row := range rows {
		if _, err := db.Exec(insert, row.key, row.value, row.code, now, now, len(row.value)); err != nil {
			t.Fatalf("inserting fixture row %s: %v", row.key, err)
		}
	}

	return path
}

// testUsername/testPassword are used for every test server's bootstrap
// account. testPassword is deliberately NOT DefaultPassword, so ordinary
// handler/mutation tests aren't accidentally subject to the
// default-credential guardrails (auth_test.go exercises those explicitly,
// using DefaultPassword on purpose).
const (
	testUsername = "admin"
	testPassword = "test-password-not-the-default"
)

// newTestAuthConfig builds an in-memory AuthConfig for tests -- no disk
// I/O, unlike LoadOrCreateAuthConfig, which is exercised separately by
// config_test.go.
func newTestAuthConfig(t *testing.T) *AuthConfig {
	t.Helper()
	secret, err := randomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	return &AuthConfig{Username: testUsername, PasswordHash: hash, SessionSecret: secret}
}

// newTestServer builds a Server over the given named database paths, with
// AllowDelete off and a fresh, non-default bootstrap account. t.Cleanup
// closes it. Use newTestServerWithConfig directly for tests that need to
// control AllowDelete, AuthConfig, or the other Config fields.
func newTestServer(t *testing.T, dbs ...DBSource) *Server {
	t.Helper()
	return newTestServerWithConfig(t, Config{Databases: dbs})
}

func newTestServerWithConfig(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.AuthConfig == nil {
		cfg.AuthConfig = newTestAuthConfig(t)
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// loginTestSession logs in against s using the test bootstrap credentials
// and returns a session cookie plus that session's CSRF token, for tests
// that exercise routes behind the auth middleware.
func loginTestSession(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	form := url.Values{"username": {testUsername}, "password": {testPassword}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("test login failed: code=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			claims, ok := s.sessions.verify(c.Value)
			if !ok {
				t.Fatal("test login issued a cookie that doesn't verify")
			}
			return c, s.csrfToken(claims)
		}
	}
	t.Fatal("test login did not set a session cookie")
	return nil, ""
}
