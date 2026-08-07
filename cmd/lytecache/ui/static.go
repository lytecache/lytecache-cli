package ui

import (
	"io/fs"
	"net/http"
)

// registerStaticRoutes mounts the embedded static/*.css and static/*.js
// files under /static/. These are the only assets a browser needs beyond
// the server-rendered HTML -- no CDN, no build step, embedded straight
// into the binary via assetsFS (see embed.go).
func (s *Server) registerStaticRoutes(mux *http.ServeMux) {
	sub, err := fs.Sub(assetsFS, "static")
	if err != nil {
		// assetsFS is compiled into the binary from ui/static/ -- a
		// missing "static" subtree here means the embed directive itself
		// is broken, not a runtime condition; fail loudly rather than
		// silently serving 404s for every asset.
		panic("ui: embedded static assets missing: " + err.Error())
	}
	// sub is already rooted at static/ (fs.Sub above), so the request's
	// "/static/" prefix must be stripped before the file server sees it,
	// or it looks for a nonexistent "static/static/style.css".
	fileServer := http.StripPrefix("/static/", http.FileServerFS(sub))
	s.handle(mux, "GET /static/{path...}", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})
}
