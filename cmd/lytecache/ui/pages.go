package ui

import "net/http"

// Page carries the fields every template's shared chrome (base.html) needs:
// the nav/user info, the database switcher, and -- on a database-scoped
// page -- the namespace sidebar with key counts. Every page-specific view
// struct embeds this.
type Page struct {
	Username    string
	CSRFToken   string
	AllowDelete bool

	Databases  []string // every configured database name, for the sidebar switcher
	CurrentDB  string
	CurrentNS  string
	Namespaces []SidebarNamespace

	// RefreshSeconds, when non-zero, makes static/poll.js auto-refresh
	// the page (pausable, see the checkbox in dashboard.html).
	RefreshSeconds int
}

// SidebarNamespace is one row of the sidebar's namespace list.
type SidebarNamespace struct {
	Name     string
	KeyCount int64
}

const defaultDashboardRefreshSeconds = 2

// basePage builds the Page common to every authenticated view: user info,
// CSRF token, and the database list for the sidebar switcher.
func (s *Server) basePage(r *http.Request) Page {
	claims, _ := s.currentSession(r)
	return Page{
		Username:    claims.Username,
		CSRFToken:   s.csrfToken(claims),
		AllowDelete: s.allowDelete,
		Databases:   s.mgr.Names(),
	}
}

// dbPage builds on basePage with the namespace sidebar for a specific,
// already-validated database -- see resolveEntry, which every caller of
// this has already gone through.
func (s *Server) dbPage(r *http.Request, dbName, ns string) Page {
	p := s.basePage(r)
	p.CurrentDB = dbName
	p.CurrentNS = ns

	e, ok := s.mgr.Entry(dbName)
	if !ok {
		return p
	}
	c, err := e.Cache("default")
	if err != nil {
		return p
	}
	infos, err := c.Namespaces()
	if err != nil {
		return p
	}
	p.Namespaces = make([]SidebarNamespace, len(infos))
	for i, info := range infos {
		p.Namespaces[i] = SidebarNamespace{Name: info.Namespace, KeyCount: info.KeyCount}
	}
	return p
}

type dashboardPage struct {
	Page
	Rows []DashboardRow
}

type keysPage struct {
	Page
	KeyBrowserView
}

type valuePage struct {
	Page
	ValueView
}

type searchPage struct {
	Page
	SearchView
}

type errorPage struct {
	Page
	Title, Message string
}

// loginPage embeds Page (zero-valued: no session exists yet) purely so
// base.html's shared chrome can reference the same fields on every page
// without erroring on a missing field -- {{if .Username}} then correctly
// hides the logged-in-only nav (user/logout) since it's empty here.
type loginPage struct {
	Page
	Error string
}

type changePasswordPage struct {
	Page
	Forced bool
	Error  string
}
