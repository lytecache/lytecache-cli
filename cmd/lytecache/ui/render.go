package ui

import (
	"bytes"
	"html/template"
)

// Every page combines templates/base.html (shared chrome: head, nav,
// sidebar) with one page-specific file via parsePage (see embed.go) --
// html/template's automatic contextual escaping matters here specifically
// because key names and values rendered by these templates are
// attacker-influenced data from a payments system's cache, not trusted
// strings.
var (
	dashboardTmpl      = parsePage("dashboard.html")
	keyBrowserTmpl     = parsePage("keys.html")
	valueViewTmpl      = parsePage("value.html")
	searchTmpl         = parsePage("search.html")
	errorTmpl          = parsePage("error.html")
	loginTmpl          = parsePage("login.html")
	changePasswordTmpl = parsePage("change_password.html")
)

// render executes tmpl's "base.html" definition against data and returns
// the rendered bytes, or an error if the template itself fails (a
// template execution error is a programming bug, not user input --
// handlers treat it as a 500).
func render(tmpl *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
