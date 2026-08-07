package ui

import (
	"html/template"
	"log"
	"net/http"
	"strconv"

	lytecache "github.com/lytecache/lytecache-go"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Every dashboard load re-checks the --scan directories for a *.db
	// file that appeared since the last scan (a service whose first cache
	// write happened after lytecache ui started, most commonly) -- see
	// Manager.Rescan's doc comment for why this can't just happen once at
	// startup. This piggybacks on the dashboard's existing 2-second
	// auto-refresh (poll.js), so a new database shows up within one
	// refresh cycle with no server restart, and costs nothing extra on
	// pages that aren't the dashboard.
	if err := s.mgr.Rescan(s.scanDirs, func(msg string) { s.logf("lytecache ui: %s", msg) }); err != nil {
		s.logf("lytecache ui: rescan: %v", err)
	}
	rows := BuildDashboard(s.mgr)
	page := s.basePage(r)
	page.RefreshSeconds = defaultDashboardRefreshSeconds
	s.writeTemplate(w, dashboardTmpl, dashboardPage{Page: page, Rows: rows})
}

func (s *Server) handleDBRedirect(w http.ResponseWriter, r *http.Request) {
	dbName := r.PathValue("db")
	if _, ok := s.mgr.entry(dbName); !ok {
		s.writeNotFound(w, r, "unknown database "+dbName)
		return
	}
	http.Redirect(w, r, "/db/"+dbName+"/namespaces/default/keys", http.StatusFound)
}

func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	view, err := BuildKeyBrowser(c, dbName, ns, q.Get("glob"), q.Get("sort"), page, size)
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	s.writeTemplate(w, keyBrowserTmpl, keysPage{Page: s.dbPage(r, dbName, ns), KeyBrowserView: view})
}

func (s *Server) handleKeyDetail(w http.ResponseWriter, r *http.Request) {
	dbName, ns, ok := s.resolveEntry(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if key == "" {
		s.writeNotFound(w, r, "missing key")
		return
	}
	c, err := entryCache(s, dbName, ns)
	if err != nil {
		s.writeEntryError(w, r, dbName, err)
		return
	}

	view, err := BuildValueView(c, dbName, ns, key, s.maskKeys)
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	s.writeTemplate(w, valueViewTmpl, valuePage{Page: s.dbPage(r, dbName, ns), ValueView: view})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("q")
	view := BuildSearch(s.mgr, pattern)
	s.writeTemplate(w, searchTmpl, searchPage{Page: s.basePage(r), SearchView: view})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// resolveEntry extracts and validates the {db}/{ns} path values shared by
// every per-database route, writing a 404 and returning ok=false if the
// database name isn't configured. It does not validate the namespace --
// an empty/never-written namespace is not an error, just an empty result.
func (s *Server) resolveEntry(w http.ResponseWriter, r *http.Request) (db, ns string, ok bool) {
	db = r.PathValue("db")
	ns = r.PathValue("ns")
	if _, exists := s.mgr.entry(db); !exists {
		s.writeNotFound(w, r, "unknown database "+db)
		return "", "", false
	}
	return db, ns, true
}

func entryCache(s *Server, db, ns string) (*lytecache.Cache, error) {
	e, _ := s.mgr.entry(db)
	return e.Cache(ns)
}

func (s *Server) writeTemplate(w http.ResponseWriter, tmpl *template.Template, data any) {
	body, err := render(tmpl, data)
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// writeEntryError renders a browsable inline error (200, not 500) for a
// configured-but-currently-unreachable database -- missing file, locked,
// incompatible schema -- so the rest of the UI (and every other configured
// database) keeps working. This is the per-page equivalent of the
// dashboard's inline error row.
func (s *Server) writeEntryError(w http.ResponseWriter, r *http.Request, dbName string, err error) {
	body, rerr := render(errorTmpl, errorPage{
		Page:    s.basePage(r),
		Title:   "Database " + dbName + " is not reachable",
		Message: err.Error(),
	})
	if rerr != nil {
		s.writeServerError(w, rerr)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	log.Printf("lytecache ui: database %q unreachable: %v", dbName, err)
}

func (s *Server) writeNotFound(w http.ResponseWriter, r *http.Request, message string) {
	body, err := render(errorTmpl, errorPage{Page: s.basePage(r), Title: "Not found", Message: message})
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(body)
}

func (s *Server) writeServerError(w http.ResponseWriter, err error) {
	log.Printf("lytecache ui: internal error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
