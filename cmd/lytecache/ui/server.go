// Package ui implements lytecache's web-based administration UI: a local
// inspection-and-cleanup tool for lytecache SQLite database files, in the
// spirit of RedisInsight/pgAdmin. It is never a cache server -- it exposes
// no cache wire protocol, and no application ever connects to it. Every
// cache operation goes through the public github.com/lytecache/lytecache-go
// API; the only exception is the read-only introspection lytecache-go's
// Cache.Namespaces/SchemaVersion/Limits/Stats now expose specifically for
// this package (see that module's CHANGELOG).
package ui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Config configures a Server. It grows across stages (metrics, masking) --
// this is what routing/data, delete-only mutations, and authentication
// need.
type Config struct {
	// Databases are explicitly named sources, already merged from a
	// config file's `databases:` list followed by --db flags (in that
	// order) -- see MergeSources for the duplicate-name rules that
	// merge should already have applied.
	Databases []DBSource
	// ScanDirs are directories to auto-discover *.db files from.
	ScanDirs []string
	// AllowDelete gates every mutating route. Deliberately not part of
	// AuthConfig/ui.yaml -- see cmd_ui.go: a config file silently
	// granting destructive capability if copied around would be exactly
	// the kind of surprise this tool should avoid, so it's CLI-flag-only.
	AllowDelete bool
	// Logf receives operational log lines (unreachable databases at
	// startup, per-request errors). Defaults to the standard logger.
	Logf func(format string, args ...any)

	// AuthConfig is the already-loaded ui.yaml contents (see
	// LoadOrCreateAuthConfig) -- required. NewServer does no config-file
	// I/O itself, keeping "where is the config file" and "print
	// first-run credentials" decisions in the caller (cmd_ui.go), and
	// keeping this package testable without touching a real filesystem
	// config directory.
	AuthConfig *AuthConfig
	// AuthConfigPath is where AuthConfig should be re-saved after a
	// password change.
	AuthConfigPath string
	// NonLoopbackBind is whether the server is being bound to anything
	// other than 127.0.0.1 -- drives the forced-password-change
	// guardrail (see auth.go). The listen address itself is cmd_ui.go's
	// concern, not this package's; only whether it's loopback matters
	// here.
	NonLoopbackBind bool
	// IdleTimeout/AbsoluteLifetime configure session expiry. Zero means
	// DefaultIdleTimeout/DefaultAbsoluteLifetime.
	IdleTimeout      time.Duration
	AbsoluteLifetime time.Duration
	// SecureCookie marks the session cookie Secure -- set this when
	// actually serving over TLS.
	SecureCookie bool
	// AuditLogPath enables the audit log at that path when non-empty.
	AuditLogPath string
	// MaskKeys are glob patterns (filepath.Match syntax) whose matching
	// keys' values are never fetched or rendered by the value viewer --
	// see MatchesMaskPattern.
	MaskKeys []string

	// NoMetrics removes the /metrics route entirely (not just guards it).
	NoMetrics bool
	// MetricsToken, when set, is required as a bearer token on /metrics.
	// CheckMetricsGuardrail (auth.go) is what actually makes this
	// mandatory once the server binds beyond loopback -- NewServer itself
	// doesn't second-guess the caller's choice here.
	MetricsToken string
	// MetricsCacheTTL bounds how often a scrape re-reads the database
	// files. Zero means DefaultMetricsCacheTTL.
	MetricsCacheTTL time.Duration
}

// Server is the lytecache ui HTTP server. It owns every open
// *lytecache.Cache for the process's lifetime (see Manager) -- callers
// must call Close when done.
type Server struct {
	mgr         *Manager
	allowDelete bool
	logf        func(format string, args ...any)
	mux         *http.ServeMux

	registeredRoutes []string

	authMu         sync.Mutex // guards authConfig, since a password change mutates and re-saves it
	authConfig     *AuthConfig
	authConfigPath string

	nonLoopbackBind bool
	sessions        *sessionManager
	rateLimiter     *loginRateLimiter
	audit           *auditLogger
	maskKeys        []string

	noMetrics       bool
	metricsToken    string
	metricsCacheTTL time.Duration
}

// NewServer builds a Server from cfg: merges the configured/scanned
// database sources, opens what it can, and wires the route table. It does
// not start listening -- call Handler() and pass it to an http.Server (or
// ListenAndServe it directly via cmd_ui.go).
func NewServer(cfg Config) (*Server, error) {
	if cfg.AuthConfig == nil {
		return nil, errors.New("ui: Config.AuthConfig is required")
	}

	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}

	sources, err := MergeSources(cfg.Databases, cfg.ScanDirs, func(msg string) { logf("lytecache ui: %s", msg) })
	if err != nil {
		return nil, err
	}

	mgr := NewManager(sources)
	mgr.WarmUp(logf)

	secret, err := base64.RawURLEncoding.DecodeString(cfg.AuthConfig.SessionSecret)
	if err != nil {
		return nil, fmt.Errorf("ui: decoding session_secret: %w", err)
	}

	idle := cfg.IdleTimeout
	if idle == 0 {
		idle = DefaultIdleTimeout
	}
	absolute := cfg.AbsoluteLifetime
	if absolute == 0 {
		absolute = DefaultAbsoluteLifetime
	}

	var audit *auditLogger
	if cfg.AuditLogPath != "" {
		audit, err = newAuditLogger(cfg.AuditLogPath)
		if err != nil {
			return nil, err
		}
	}

	s := &Server{
		mgr:             mgr,
		allowDelete:     cfg.AllowDelete,
		logf:            logf,
		authConfig:      cfg.AuthConfig,
		authConfigPath:  cfg.AuthConfigPath,
		nonLoopbackBind: cfg.NonLoopbackBind,
		sessions:        newSessionManager(secret, idle, absolute, cfg.SecureCookie),
		rateLimiter:     newLoginRateLimiter(),
		audit:           audit,
		maskKeys:        cfg.MaskKeys,
		noMetrics:       cfg.NoMetrics,
		metricsToken:    cfg.MetricsToken,
		metricsCacheTTL: cfg.MetricsCacheTTL,
	}
	s.mux = s.routes()
	return s, nil
}

// Handler returns the server's http.Handler: security headers wrapping
// the auth/CSRF middleware wrapping the route table.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.authMiddleware(s.mux))
}

// Close closes every database this server opened and the audit log, if
// enabled.
func (s *Server) Close() error {
	var errs []error
	if err := s.mgr.Close(); err != nil {
		errs = append(errs, err)
	}
	if s.audit != nil {
		if err := s.audit.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
