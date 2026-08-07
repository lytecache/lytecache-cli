package ui

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookieName = "lytecache_session"

// DefaultIdleTimeout/DefaultAbsoluteLifetime are the spec's defaults for
// session expiry, both configurable via Config.
const (
	DefaultIdleTimeout      = 30 * time.Minute
	DefaultAbsoluteLifetime = 12 * time.Hour
)

// sessionClaims is the signed payload carried by the session cookie.
type sessionClaims struct {
	ID        string    `json:"id"` // random, used only for server-side revocation
	Username  string    `json:"username"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"` // absolute lifetime cutoff, fixed at issue time
	LastSeen  time.Time `json:"last_seen"`  // idle-timeout base, refreshed on every request
}

// sessionManager issues, verifies, refreshes, and revokes session cookies.
// Cookies are signed (HMAC-SHA256) with the config's session_secret, not
// server-side session state, so a restart doesn't invalidate live sessions
// -- only an explicit logout (which revokes by ID) or the secret itself
// changing does.
type sessionManager struct {
	secret       []byte
	idleTimeout  time.Duration
	absoluteLife time.Duration
	secureCookie bool

	mu      sync.Mutex
	revoked map[string]time.Time // sessionID -> that session's ExpiresAt, so entries can be GC'd once they'd have expired anyway
}

func newSessionManager(secret []byte, idleTimeout, absoluteLife time.Duration, secureCookie bool) *sessionManager {
	return &sessionManager{
		secret:       secret,
		idleTimeout:  idleTimeout,
		absoluteLife: absoluteLife,
		secureCookie: secureCookie,
		revoked:      make(map[string]time.Time),
	}
}

// issue creates a new session for username and sets its cookie on w.
func (m *sessionManager) issue(w http.ResponseWriter, username string) error {
	id, err := randomToken(16)
	if err != nil {
		return err
	}
	now := time.Now()
	claims := sessionClaims{
		ID: id, Username: username,
		IssuedAt: now, ExpiresAt: now.Add(m.absoluteLife), LastSeen: now,
	}
	return m.write(w, claims)
}

// write (re-)sets the session cookie for claims, e.g. to refresh LastSeen
// on every authenticated request (sliding idle timeout).
func (m *sessionManager) write(w http.ResponseWriter, claims sessionClaims) error {
	value, err := m.encode(claims)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   m.secureCookie,
		Expires:  claims.ExpiresAt,
	})
	return nil
}

func (m *sessionManager) encode(claims sessionClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	return encodedPayload + "." + m.sign(encodedPayload), nil
}

func (m *sessionManager) sign(encodedPayload string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(encodedPayload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verify checks a cookie value's signature, expiry, idle timeout, and
// revocation status, returning the claims if every check passes.
func (m *sessionManager) verify(cookieValue string) (sessionClaims, bool) {
	encodedPayload, sig, ok := strings.Cut(cookieValue, ".")
	if !ok {
		return sessionClaims{}, false
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(m.sign(encodedPayload))) != 1 {
		return sessionClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return sessionClaims{}, false
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, false
	}

	now := time.Now()
	if now.After(claims.ExpiresAt) || now.Sub(claims.LastSeen) > m.idleTimeout {
		return sessionClaims{}, false
	}

	m.mu.Lock()
	_, isRevoked := m.revoked[claims.ID]
	m.mu.Unlock()
	if isRevoked {
		return sessionClaims{}, false
	}

	return claims, true
}

// revoke invalidates claims server-side (logout) -- a copy of the cookie
// taken before logout can no longer be used, unlike simply deleting the
// cookie client-side, which does nothing to a copy an attacker already has.
func (m *sessionManager) revoke(claims sessionClaims) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[claims.ID] = claims.ExpiresAt
	now := time.Now()
	for id, exp := range m.revoked {
		if now.After(exp) {
			delete(m.revoked, id)
		}
	}
}
