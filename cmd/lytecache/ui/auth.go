package ui

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type sessionCtxKeyType struct{}

var sessionCtxKey sessionCtxKeyType

// currentSession returns the authenticated session's claims, if any (set
// by authMiddleware after a successful cookie check).
func (s *Server) currentSession(r *http.Request) (sessionClaims, bool) {
	claims, ok := r.Context().Value(sessionCtxKey).(sessionClaims)
	return claims, ok
}

// IsLoopbackHost reports whether host (as given to --host, before a port
// is appended) refers only to this machine -- the boundary the spec's
// exposure guardrails key off of.
func IsLoopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckStartupGuardrails enforces the spec's mandatory, non-waivable
// exposure rules before the server ever starts listening: binding beyond
// loopback with the default password is refused outright (not just
// nagged about), and binding beyond loopback at all requires either TLS
// or an explicit --insecure acknowledgement.
func CheckStartupGuardrails(host string, cfg *AuthConfig, tlsConfigured, insecure bool) error {
	if IsLoopbackHost(host) {
		return nil
	}
	if cfg.IsDefaultPassword() {
		return fmt.Errorf("refusing to bind %s with the default admin/admin password still set -- "+
			"run 'lytecache ui passwd' first, or omit --host to stay on 127.0.0.1", host)
	}
	if !tlsConfigured && !insecure {
		return fmt.Errorf("refusing to bind %s without TLS -- pass --tls-cert/--tls-key, "+
			"or acknowledge the risk explicitly with --insecure", host)
	}
	return nil
}

// CheckMetricsGuardrail enforces the spec's /metrics-specific exposure
// rule: bound to loopback, /metrics may be scraped unauthenticated (a
// Prometheus scraper can't do an interactive login); bound beyond
// loopback, a --metrics-token is mandatory, and its absence refuses
// startup outright rather than silently serving an unauthenticated
// metrics endpoint to the world. A no-op when metrics are disabled
// entirely (--no-metrics).
func CheckMetricsGuardrail(host string, noMetrics bool, token string) error {
	if noMetrics || IsLoopbackHost(host) {
		return nil
	}
	if token == "" {
		return fmt.Errorf("refusing to bind %s with /metrics enabled and no --metrics-token -- "+
			"pass --metrics-token, or disable metrics with --no-metrics", host)
	}
	return nil
}

// forcedPasswordChangeRequired implements the second, defense-in-depth
// guardrail: even though CheckStartupGuardrails already prevents binding
// non-loopback with the default password, the password can be reset (via
// `lytecache ui reset-password`) while the server is already running --
// this is checked on every request, not cached, so that takes effect
// immediately.
func (s *Server) forcedPasswordChangeRequired() bool {
	if !s.nonLoopbackBind {
		return false
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.authConfig.IsDefaultPassword()
}

// authExemptPaths never require a session -- a login page you can only
// reach by already being logged in isn't a login page, and a metrics
// scraper/health check can't perform an interactive login.
func authExempt(path string) bool {
	switch path {
	case "/login", "/healthz", "/metrics":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

// authMiddleware enforces "every route except /login and static assets
// requires a valid session" as middleware, not a per-handler check, per
// the spec. It also implements the forced-password-change redirect and
// CSRF verification for state-changing requests, so neither can be
// accidentally skipped by a route that forgets to call a helper.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			s.redirectToLogin(w, r)
			return
		}
		claims, ok := s.sessions.verify(cookie.Value)
		if !ok {
			s.redirectToLogin(w, r)
			return
		}

		// Sliding idle timeout: touch LastSeen on every authenticated
		// request, re-signing the cookie.
		claims.LastSeen = time.Now()
		_ = s.sessions.write(w, claims)

		if s.forcedPasswordChangeRequired() && r.URL.Path != "/change-password" {
			http.Redirect(w, r, "/change-password", http.StatusFound)
			return
		}

		if isStateChanging(r.Method) {
			if !s.verifyCSRF(r, claims) {
				http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
				return
			}
		}

		ctx := context.WithValue(r.Context(), sessionCtxKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusFound)
}

// csrfToken derives a per-session CSRF token from the session's own ID via
// HMAC keyed by the same session_secret used to sign the cookie itself --
// a double-submit token, not server-side per-request state.
func (s *Server) csrfToken(claims sessionClaims) string {
	mac := hmac.New(sha256.New, s.sessions.secret)
	mac.Write([]byte("csrf:" + claims.ID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifyCSRF(r *http.Request, claims sessionClaims) bool {
	token := r.FormValue("csrf_token")
	expected := s.csrfToken(claims)
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// securityHeaders sets a strict CSP (no inline scripts -- every template
// loads JS from /static/*.js, never an inline <script>) and the other
// headers the spec calls for, on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the request's remote IP (without port) for rate
// limiting and audit logging.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
