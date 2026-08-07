package ui

import "net/http"

// routes builds the full route table. Go 1.22+'s enhanced ServeMux
// (method-prefixed patterns, {param} path values, {param...} wildcards)
// is sufficient for every route the spec calls for -- no external router
// dependency is needed. It also gives us 405 Method Not Allowed for free:
// a request whose path matches a registered pattern but whose method
// doesn't is rejected automatically, without any handler code -- exactly
// what TestNoRouteCanCreateOrModifyAValue (mutations_test.go) relies on.
//
// {key...} (not {key}) is deliberate: a cache key is an arbitrary string
// and may itself contain "/", which a plain {key} segment (bounded to one
// path component) would truncate.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	s.handle(mux, "GET /{$}", s.handleIndex)
	s.handle(mux, "GET /dashboard", s.handleDashboard)
	s.handle(mux, "GET /db/{db}", s.handleDBRedirect)
	s.handle(mux, "GET /db/{db}/namespaces/{ns}/keys", s.handleKeys)
	s.handle(mux, "GET /db/{db}/namespaces/{ns}/keys/{key...}", s.handleKeyDetail)
	s.handle(mux, "GET /search", s.handleSearch)
	s.handle(mux, "GET /healthz", s.handleHealthz)

	s.handle(mux, "GET /login", s.handleLoginForm)
	s.handle(mux, "POST /login", s.handleLoginSubmit)
	s.handle(mux, "POST /logout", s.handleLogout)
	s.handle(mux, "GET /change-password", s.handleChangePasswordForm)
	s.handle(mux, "POST /change-password", s.handleChangePasswordSubmit)

	s.registerMutationRoutes(mux) // Stage 1.2, gated behind AllowDelete
	s.registerStaticRoutes(mux)   // Stage 3
	s.registerMetricsRoute(mux)   // Stage 4, no-op if NoMetrics

	return mux
}

// handle registers pattern (a "METHOD /path" string, as ServeMux expects)
// and records it, so RegisteredRoutes can enumerate the exact route table
// for tests -- notably TestNoRouteCanCreateOrModifyAValue, which needs to
// see every route that exists, not just the ones a handwritten list
// happens to remember to check.
func (s *Server) handle(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, handler)
	s.registeredRoutes = append(s.registeredRoutes, pattern)
}

// RegisteredRoutes returns every "METHOD /path" pattern this server has
// registered, in registration order.
func (s *Server) RegisteredRoutes() []string {
	out := make([]string, len(s.registeredRoutes))
	copy(out, s.registeredRoutes)
	return out
}
