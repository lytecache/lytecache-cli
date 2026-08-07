package ui

import (
	"fmt"
	"net/http"
	"strings"
)

// registerMutationRoutes mounts the delete-only routes: single/bulk key
// delete, namespace flush, and a maintenance pass, all gated behind
// --allow-delete, plus vacuum, which is not gated.
//
// Deliberately absent from this file, on purpose, permanently: any route
// that creates or edits a value, or sets/extends/clears a TTL. This isn't
// a permissions check that a crafted request could bypass -- the handler
// simply doesn't exist, so there is nothing to reach. These caches back a
// payments system; a UI able to inject or alter a cached record (an OTP,
// an amount, an idempotency claim) would be an authorization bypass,
// whereas deletion is always safe, since a cache entry can only ever hold
// something that's recomputable or re-fetchable by definition. See
// mutations_test.go's TestNoRouteCanCreateOrModifyAValue, which enumerates
// the live route table to keep this true even as the package grows.
//
// The key/keys to delete are POST body fields, not URL path segments --
// this sidesteps a real routing conflict: a key may contain "/", which
// needs the trailing {key...} wildcard (see routes.go), but Go's ServeMux
// wildcards must be the last segment of a pattern, so a literal action
// suffix like ".../keys/{key}/delete" can't follow one.
func (s *Server) registerMutationRoutes(mux *http.ServeMux) {
	s.handle(mux, "POST /db/{db}/namespaces/{ns}/delete-key", s.requireAllowDelete(s.handleDeleteKey))
	s.handle(mux, "POST /db/{db}/namespaces/{ns}/delete-keys", s.requireAllowDelete(s.handleDeleteKeys))
	s.handle(mux, "POST /db/{db}/namespaces/{ns}/flush", s.requireAllowDelete(s.handleFlush))
	s.handle(mux, "POST /db/{db}/namespaces/{ns}/maintain", s.requireAllowDelete(s.handleMaintain))
	// Vacuum reclaims free space left by already-deleted rows; it removes
	// no cache data of its own, so it is intentionally not gated behind
	// --allow-delete -- gating it would protect against the wrong thing.
	s.handle(mux, "POST /db/{db}/namespaces/{ns}/vacuum", s.handleVacuum)
}

// requireAllowDelete wraps a mutating handler so the 403 is enforced
// server-side, unconditionally -- never something the frontend alone
// decides. Every mutating route (other than vacuum, see above) goes
// through this.
func (s *Server) requireAllowDelete(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.allowDelete {
			http.Error(w, "deletion is disabled -- start lytecache ui with --allow-delete to enable it", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeBadRequest(w, r, err.Error())
		return
	}
	key := r.PostForm.Get("key")
	if key == "" {
		s.writeBadRequest(w, r, "missing key")
		return
	}

	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}
	n, err := c.Delete(key)
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	s.logMutation(r, "delete", dbName, ns, key, fmt.Sprintf("deleted %d key(s)", n))
	s.redirectToKeys(w, r, dbName, ns, fmt.Sprintf("deleted %d key(s)", n))
}

func (s *Server) handleDeleteKeys(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeBadRequest(w, r, err.Error())
		return
	}
	keys := r.PostForm["key"]
	if len(keys) == 0 {
		s.writeBadRequest(w, r, "no keys selected")
		return
	}

	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}
	n, err := c.Delete(keys...)
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	s.logMutation(r, "delete-bulk", dbName, ns, strings.Join(keys, ","), fmt.Sprintf("%d of %d requested", n, len(keys)))
	s.redirectToKeys(w, r, dbName, ns, fmt.Sprintf("deleted %d of %d selected key(s)", n, len(keys)))
}

// handleFlush requires the operator to type the namespace name back,
// matching the spec's "typed confirmation" requirement -- a single
// checkbox is too easy to click through for an operation this
// irreversible-in-effect (even though the data itself is, by policy,
// always recomputable).
func (s *Server) handleFlush(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeBadRequest(w, r, err.Error())
		return
	}
	if r.PostForm.Get("confirm_namespace") != ns {
		s.writeBadRequest(w, r, "confirmation text did not match the namespace name")
		return
	}

	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}
	if err := c.Flush(); err != nil {
		s.writeServerError(w, err)
		return
	}
	s.logMutation(r, "flush", dbName, ns, "", "")
	s.redirectToKeys(w, r, dbName, ns, "namespace flushed")
}

func (s *Server) handleMaintain(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}
	result, err := c.Maintain()
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	s.logMutation(r, "maintain", dbName, ns, "", fmt.Sprintf("expired_removed=%d evicted=%d", result.ExpiredRemoved, result.Evicted))
	s.redirectToKeys(w, r, dbName, ns, fmt.Sprintf("maintenance pass: %d expired removed, %d evicted", result.ExpiredRemoved, result.Evicted))
}

func (s *Server) handleVacuum(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}
	if err := c.Vacuum(); err != nil {
		s.writeServerError(w, err)
		return
	}
	s.logMutation(r, "vacuum", dbName, ns, "", "")
	s.redirectToKeys(w, r, dbName, ns, "vacuum complete")
}

func (s *Server) redirectToKeys(w http.ResponseWriter, r *http.Request, dbName, ns, _ string) {
	// The trailing message argument is accepted now and will drive a
	// Stage 3 flash-message banner; for now the redirect itself is the
	// only user-visible feedback.
	http.Redirect(w, r, "/db/"+dbName+"/namespaces/"+ns+"/keys", http.StatusSeeOther)
}

// logMutation records a write action to both the operational log and the
// persistent audit log: timestamp, username, remote IP, action, database,
// namespace, key -- never a value. key may be empty (flush/maintain/
// vacuum act on a whole namespace, not one key) or a comma-joined list
// (bulk delete).
func (s *Server) logMutation(r *http.Request, action, db, ns, key, detail string) {
	username := "-"
	if claims, ok := s.currentSession(r); ok {
		username = claims.Username
	}
	s.logf("lytecache ui: audit action=%s db=%s namespace=%s key=%q user=%s remote=%s %s",
		action, db, ns, key, username, clientIP(r), detail)
	if s.audit != nil {
		s.audit.log(username, clientIP(r), action, db, ns, key)
	}
}

func (s *Server) writeBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	body, err := render(errorTmpl, errorPage{Page: s.basePage(r), Title: "Bad request", Message: message})
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(body)
}
