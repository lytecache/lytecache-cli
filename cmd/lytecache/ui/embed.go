package ui

import (
	"embed"
	"html/template"
)

// assetsFS embeds every template and static asset directly into the
// lytecache binary -- go build produces one static binary with a working
// UI, no external files, no Node.js build step. Static assets are plain
// hand-written CSS/JS (see static/*.css, static/*.js); if that ever
// changes, document the rebuild command here.
//
//go:embed templates static
var assetsFS embed.FS

// parsePage combines templates/base.html (the shared chrome: head, nav,
// theme CSS link) with one page-specific template file into a single
// *template.Template, per the standard html/template "base + named
// blocks" composition idiom. Each page file defines "title" and "content"
// blocks that base.html invokes.
func parsePage(name string) *template.Template {
	return template.Must(template.New("base.html").ParseFS(assetsFS, "templates/base.html", "templates/"+name))
}
