package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUnauthenticatedRequestRedirectsToLogin(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestLoginPageAndStaticAssetsAreAuthExempt(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/login", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code == http.StatusFound && rec.Header().Get("Location") == "/login" {
			t.Errorf("%s should be auth-exempt, got redirected to /login", path)
		}
	}
}

func TestSuccessfulLoginIssuesAWorkingSession(t *testing.T) {
	s := newTestServer(t)
	cookie, _ := loginTestSession(t, s)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated GET /dashboard: code = %d, want 200", rec.Code)
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	s := newTestServer(t)
	form := url.Values{"username": {testUsername}, "password": {"definitely-wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			t.Error("a failed login must not issue a working session cookie")
		}
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	s := newTestServer(t)
	cookie, csrfToken := loginTestSession(t, s)

	logoutReq := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
	logoutReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutReq.AddCookie(cookie)
	logoutRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusFound {
		t.Fatalf("logout: code = %d, want 302", logoutRec.Code)
	}

	// The *original* cookie -- as a copy an attacker might have taken
	// before logout -- must no longer work.
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Errorf("a revoked session cookie should be rejected like no cookie at all; code=%d location=%q",
			rec.Code, rec.Header().Get("Location"))
	}
}

func TestCSRFRejectsMissingOrWrongToken(t *testing.T) {
	path := tempDBPath(t)
	mustSeedCache(t, path, "k", "v")

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	cookie, _ := loginTestSession(t, s)

	cases := []struct {
		name  string
		token string
	}{
		{"missing token", ""},
		{"wrong token", "not-the-real-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"key": {"k"}}
			if tc.token != "" {
				form.Set("csrf_token", tc.token)
			}
			req := httptest.NewRequest(http.MethodPost, "/db/svc/namespaces/default/delete-key", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("code = %d, want 403", rec.Code)
			}
		})
	}

	if found, err := c2(t, path).Exists("k"); err != nil || !found {
		t.Error("key must survive a CSRF-rejected delete request")
	}
}

func TestCSRFAcceptsValidToken(t *testing.T) {
	path := tempDBPath(t)
	mustSeedCache(t, path, "k", "v")

	s := newDeletableTestServer(t, DBSource{Name: "svc", Path: path})
	rec := doPost(t, s, "/db/svc/namespaces/default/delete-key", url.Values{"key": {"k"}})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("code = %d, want 303 -- doPost's CSRF token should be accepted", rec.Code)
	}
}

func TestLoginRateLimitTriggersAfterRepeatedFailures(t *testing.T) {
	s := newTestServer(t)

	var lastCode int
	for i := 0; i < failuresBeforeBackoff+2; i++ {
		form := url.Values{"username": {testUsername}, "password": {"wrong"}}
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.7:12345"
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("after %d failures, code = %d, want 429", failuresBeforeBackoff+2, lastCode)
	}

	// A different client IP must not be affected by another IP's lockout.
	form := url.Values{"username": {testUsername}, "password": {testPassword}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.9:12345"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("a different IP's correct login: code = %d, want 302 (not affected by another IP's lockout)", rec.Code)
	}
}

// --- Default-credential guardrail matrix (handler-level) ---------------------------------------------------

func TestLoopbackWithDefaultPasswordReachesDashboardNoForcedChange(t *testing.T) {
	authCfg := &AuthConfig{Username: DefaultUsername, PasswordHash: mustHash(t, DefaultPassword), SessionSecret: mustSecret(t)}
	s := newTestServerWithConfig(t, Config{AuthConfig: authCfg, NonLoopbackBind: false})

	form := url.Values{"username": {DefaultUsername}, "password": {DefaultPassword}}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound || loginRec.Header().Get("Location") != "/dashboard" {
		t.Fatalf("login with default credentials on loopback: code=%d location=%q", loginRec.Code, loginRec.Header().Get("Location"))
	}

	var cookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie issued")
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("dashboard on loopback with default password: code = %d, want 200 (no forced redirect)", rec.Code)
	}
}

func TestNonLoopbackBindWithDefaultPasswordForcesChangeRedirect(t *testing.T) {
	authCfg := &AuthConfig{Username: DefaultUsername, PasswordHash: mustHash(t, DefaultPassword), SessionSecret: mustSecret(t)}
	s := newTestServerWithConfig(t, Config{AuthConfig: authCfg, NonLoopbackBind: true})
	cookie, _ := loginAs(t, s, DefaultUsername, DefaultPassword)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/change-password" {
		t.Fatalf("code=%d location=%q, want a redirect to /change-password", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAfterPasswordChangeRestrictionsLift(t *testing.T) {
	path := tempDBPath(t)
	seed, err := newTestCacheAt(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()

	authCfg := &AuthConfig{Username: DefaultUsername, PasswordHash: mustHash(t, DefaultPassword), SessionSecret: mustSecret(t)}
	authPath := tempConfigPath(t)
	s := newTestServerWithConfig(t, Config{AuthConfig: authCfg, AuthConfigPath: authPath, NonLoopbackBind: true})
	cookie, _ := loginAs(t, s, DefaultUsername, DefaultPassword)

	// Still forced, before the change.
	blocked := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	blocked.AddCookie(cookie)
	blockedRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(blockedRec, blocked)
	if blockedRec.Code != http.StatusFound {
		t.Fatalf("expected the forced redirect before the change, got code=%d", blockedRec.Code)
	}

	// Fetch a CSRF token via the (accessible-while-forced) change-password
	// page, then submit a real change.
	formReq := httptest.NewRequest(http.MethodGet, "/change-password", nil)
	formReq.AddCookie(cookie)
	formRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(formRec, formReq)
	claims, ok := s.sessions.verify(cookie.Value)
	if !ok {
		t.Fatal("session should still verify")
	}
	token := s.csrfToken(claims)

	changeForm := url.Values{"new_password": {"a-real-password-now"}, "csrf_token": {token}}
	changeReq := httptest.NewRequest(http.MethodPost, "/change-password", strings.NewReader(changeForm.Encode()))
	changeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changeReq.AddCookie(cookie)
	changeRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(changeRec, changeReq)
	if changeRec.Code != http.StatusFound || changeRec.Header().Get("Location") != "/dashboard" {
		t.Fatalf("password change: code=%d location=%q", changeRec.Code, changeRec.Header().Get("Location"))
	}

	// Now unblocked, same session.
	afterReq := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	afterReq.AddCookie(cookie)
	afterRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(afterRec, afterReq)
	if afterRec.Code != http.StatusOK {
		t.Errorf("after password change: code = %d, want 200 (restriction should have lifted)", afterRec.Code)
	}
}

func loginAs(t *testing.T, s *Server, username, password string) (*http.Cookie, string) {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("loginAs(%q): code=%d body=%s", username, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			claims, ok := s.sessions.verify(c.Value)
			if !ok {
				t.Fatal("issued cookie did not verify")
			}
			return c, s.csrfToken(claims)
		}
	}
	t.Fatal("no session cookie set")
	return nil, ""
}

func mustSecret(t *testing.T) string {
	t.Helper()
	secret, err := randomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func tempConfigPath(t *testing.T) string {
	t.Helper()
	return tempDBPath(t) + ".ui.yaml"
}

func mustSeedCache(t *testing.T, path, key, value string) {
	t.Helper()
	c, err := newTestCacheAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set(key, value); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
