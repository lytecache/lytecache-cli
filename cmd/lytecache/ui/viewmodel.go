package ui

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	lytecache "github.com/lytecache/lytecache-go"
)

// --- Fleet dashboard ---------------------------------------------------

// DashboardRow summarizes one configured database for the fleet dashboard.
type DashboardRow struct {
	Name string
	Path string

	// Err is set (and every other field left zero) if this database could
	// not be read at all -- missing file, locked, incompatible schema.
	Err string

	NamespaceCount int
	KeyCount       int64
	SizeBytes      int64
	MaxBytes       *int64
	MaxKeys        *int64
	HitRate        float64
	Evictions      int64
	ExpiredRemoved int64
	ExpiredPresent int64
	SchemaVersion  int
	ModTime        time.Time

	Unhealthy    bool
	UnhealthyWhy []string
}

// MaxBytesDisplay renders MaxBytes for the dashboard table, or "unlimited"
// when no cap was declared.
func (r DashboardRow) MaxBytesDisplay() string {
	if r.MaxBytes == nil {
		return "unlimited"
	}
	return strconv.FormatInt(*r.MaxBytes, 10)
}

// MaxKeysDisplay renders MaxKeys for the dashboard table, or "unlimited"
// when no cap was declared.
func (r DashboardRow) MaxKeysDisplay() string {
	if r.MaxKeys == nil {
		return "unlimited"
	}
	return strconv.FormatInt(*r.MaxKeys, 10)
}

// HitRatePercent renders HitRate as a percentage string for the dashboard
// table.
func (r DashboardRow) HitRatePercent() string {
	return fmt.Sprintf("%.1f%%", r.HitRate*100)
}

// unhealthyThreshold flags a database as unhealthy once usage crosses this
// fraction of a configured cap.
const unhealthyThreshold = 0.85

func firstNonNil(a, b *int64) *int64 {
	if a != nil {
		return a
	}
	return b
}

// BuildDashboard computes one DashboardRow per configured database. A
// database that fails to open or fails a stats/namespace query degrades to
// an error row rather than aborting the whole dashboard.
func BuildDashboard(mgr *Manager) []DashboardRow {
	names := mgr.Names()
	rows := make([]DashboardRow, 0, len(names))
	for _, name := range names {
		rows = append(rows, buildDashboardRow(mgr, name))
	}
	return rows
}

func buildDashboardRow(mgr *Manager, name string) DashboardRow {
	e, _ := mgr.entry(name)
	row := DashboardRow{Name: name, Path: e.Path}

	c, err := e.Cache("default")
	if err != nil {
		row.Err = err.Error()
		return row
	}

	nsInfos, err := c.Namespaces()
	if err != nil {
		row.Err = err.Error()
		return row
	}
	row.NamespaceCount = len(nsInfos)
	for _, ns := range nsInfos {
		row.KeyCount += ns.KeyCount
		row.SizeBytes += ns.SizeBytes
		row.ExpiredPresent += ns.ExpiredPresent
	}

	if stats, err := c.Stats(); err == nil {
		row.HitRate = stats.HitRate
		row.Evictions = stats.Evictions
		row.ExpiredRemoved = stats.ExpiredRemoved
	}

	// c.Limits() reflects only this Cache's own configuration -- since the
	// UI never opens with WithMaxKeys/WithMaxBytes itself, this is
	// normally nil; the operator-declared hint (DBSource.MaxKeys/MaxBytes)
	// is what actually drives the usage bars in practice. See DBSource's
	// doc comment for why limits can't be discovered from the file.
	row.MaxBytes = firstNonNil(e.DeclaredMaxBytes, c.Limits().MaxBytes)
	row.MaxKeys = firstNonNil(e.DeclaredMaxKeys, c.Limits().MaxKeys)
	row.SchemaVersion = c.SchemaVersion()

	if fi, statErr := os.Stat(e.Path); statErr == nil {
		row.ModTime = fi.ModTime()
	}

	row.Unhealthy, row.UnhealthyWhy = evaluateHealth(row)
	return row
}

// evaluateHealth flags the signals called out in the spec: usage above
// ~85% of a configured cap, and a non-zero (and thus worth investigating)
// expired-but-present count. Eviction/expiry counts are shown as raw
// numbers rather than compared against a prior poll -- a stateless
// per-request computation has no previous sample to diff against; a
// meaningful "rising" trend needs the poller (dashboard auto-refresh or a
// Prometheus rate() query) to compare across time, not this function.
func evaluateHealth(row DashboardRow) (bool, []string) {
	var reasons []string
	if row.MaxBytes != nil && *row.MaxBytes > 0 {
		if pct := float64(row.SizeBytes) / float64(*row.MaxBytes); pct >= unhealthyThreshold {
			reasons = append(reasons, fmt.Sprintf("size at %.0f%% of max_bytes", pct*100))
		}
	}
	if row.MaxKeys != nil && *row.MaxKeys > 0 {
		if pct := float64(row.KeyCount) / float64(*row.MaxKeys); pct >= unhealthyThreshold {
			reasons = append(reasons, fmt.Sprintf("keys at %.0f%% of max_keys", pct*100))
		}
	}
	if row.ExpiredPresent > 0 {
		reasons = append(reasons, fmt.Sprintf("%d expired key(s) not yet swept", row.ExpiredPresent))
	}
	return len(reasons) > 0, reasons
}

// --- Key browser ---------------------------------------------------

// KeyRow is one row of the key browser table.
type KeyRow struct {
	Key          string
	TypeName     string
	NonPortable  bool
	TTLRemaining *time.Duration
	// ExpiresAt is the absolute instant, alongside TTLRemaining's
	// snapshot-at-render-time duration -- the live client-side countdown
	// (static/countdown.js) recomputes remaining time from this on an
	// interval, since a duration string frozen at render time goes stale
	// the moment the page finishes loading.
	ExpiresAt    *time.Time
	SizeBytes    int64
	AccessCount  int64
	CreatedAt    time.Time
	LastAccessed time.Time
}

// TTLDisplay renders a snapshot-at-render-time TTL string ("-", "expired",
// or a duration) for the key browser table; static/countdown.js takes over
// from ExpiresAt client-side once the page has loaded.
func (r KeyRow) TTLDisplay() string {
	if r.TTLRemaining == nil {
		return "-"
	}
	if *r.TTLRemaining <= 0 {
		return "expired"
	}
	return r.TTLRemaining.Round(time.Second).String()
}

// ExpiresAtRFC3339 renders ExpiresAt for the data-expires-at attribute
// countdown.js reads, or "" if the key has no TTL.
func (r KeyRow) ExpiresAtRFC3339() string {
	if r.ExpiresAt == nil {
		return ""
	}
	return r.ExpiresAt.Format(time.RFC3339)
}

// KeyBrowserView is the full key-browser page's data.
//
// PageNum (not "Page"): keysPage (pages.go) embeds both this type and the
// shared chrome type ui.Page side by side, and Go's field-promotion rules
// resolve an ambiguous name in favor of whichever embedded field is
// literally named that -- ui.Page's own promoted field is named "Page"
// (matching its type name), which unconditionally wins over anything
// merely nested inside KeyBrowserView at the same depth, silently
// shadowing a same-named field here rather than erroring. html/template's
// reflection-based {{.Page}} would resolve to the wrong one with no
// compile-time warning; naming this PageNum sidesteps the collision
// entirely instead of relying on template authors to remember the trap.
type KeyBrowserView struct {
	DB, Namespace string
	Glob          string
	Sort          string
	PageNum       int
	PageSize      int
	TotalCount    int
	Truncated     bool
	Rows          []KeyRow
}

// SortLink builds the URL for a clickable column header: sorting by an
// already-active field toggles direction; sorting by a new field starts
// ascending.
func (v KeyBrowserView) SortLink(field string) string {
	next := field
	if v.Sort == field {
		next = "-" + field
	}
	u := url.URL{
		Path:     "/db/" + v.DB + "/namespaces/" + v.Namespace + "/keys",
		RawQuery: url.Values{"glob": {v.Glob}, "sort": {next}}.Encode(),
	}
	return u.String()
}

// SortIndicator returns an arrow for the currently-sorted column header,
// or "" for every other column.
func (v KeyBrowserView) SortIndicator(field string) string {
	switch v.Sort {
	case field:
		return "▲" // ▲
	case "-" + field:
		return "▼" // ▼
	}
	return ""
}

// maxKeysScanned bounds how many keys a single glob match will walk before
// giving up and reporting the result as truncated -- protects against an
// unbounded scan over a namespace with millions of keys and no filter.
const maxKeysScanned = 20000

const defaultPageSize = 50

// BuildKeyBrowser lists, filters, sorts, and paginates keys in c's
// namespace. Sorting/pagination happen in memory after fetching every
// glob-matched key (bounded by maxKeysScanned) -- the library's Keys
// iterator does keyset pagination internally but does not expose a
// resumable cursor across separate calls, so there is no way to ask for
// "rows 501-550" directly without walking from the start each time.
func BuildKeyBrowser(c *lytecache.Cache, db, namespace, glob, sortBy string, page, pageSize int) (KeyBrowserView, error) {
	if glob == "" {
		glob = "*"
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if page <= 0 {
		page = 1
	}

	var keys []string
	truncated := false
	for k, err := range c.Keys(glob) {
		if err != nil {
			return KeyBrowserView{}, err
		}
		keys = append(keys, k)
		if len(keys) >= maxKeysScanned {
			truncated = true
			break
		}
	}

	rows := make([]KeyRow, 0, len(keys))
	for _, k := range keys {
		info, found, err := c.Inspect(k)
		if err != nil {
			return KeyBrowserView{}, err
		}
		if !found {
			// Expired between Keys() yielding it and this Inspect call --
			// Keys() itself doesn't proactively delete, so this is
			// expected under concurrent access, not an error.
			continue
		}
		row := KeyRow{
			Key:          k,
			TypeName:     typeCodeName(info.ValueType),
			NonPortable:  isNonPortable(info.ValueType),
			SizeBytes:    info.SizeBytes,
			AccessCount:  info.AccessCount,
			CreatedAt:    info.CreatedAt,
			LastAccessed: info.LastAccessed,
		}
		if info.ExpiresAt != nil {
			d := time.Until(*info.ExpiresAt)
			row.TTLRemaining = &d
			row.ExpiresAt = info.ExpiresAt
		}
		rows = append(rows, row)
	}

	sortKeyRows(rows, sortBy)

	total := len(rows)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return KeyBrowserView{
		DB: db, Namespace: namespace, Glob: glob, Sort: sortBy,
		PageNum: page, PageSize: pageSize, TotalCount: total, Truncated: truncated,
		Rows: rows[start:end],
	}, nil
}

// sortKeyRows sorts in place. sortBy is a field name ("key", "size",
// "access_count", "created", "last_accessed", "ttl"), optionally prefixed
// with "-" for descending; an unrecognized/empty value sorts by key
// ascending.
func sortKeyRows(rows []KeyRow, sortBy string) {
	desc := strings.HasPrefix(sortBy, "-")
	field := strings.TrimPrefix(sortBy, "-")

	less := func(i, j int) bool {
		switch field {
		case "size":
			return rows[i].SizeBytes < rows[j].SizeBytes
		case "access_count":
			return rows[i].AccessCount < rows[j].AccessCount
		case "created":
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		case "last_accessed":
			return rows[i].LastAccessed.Before(rows[j].LastAccessed)
		case "ttl":
			// No-expiry keys sort last regardless of direction, so
			// reversing the direction never buries them in the middle.
			if rows[i].TTLRemaining == nil {
				return false
			}
			if rows[j].TTLRemaining == nil {
				return true
			}
			return *rows[i].TTLRemaining < *rows[j].TTLRemaining
		default:
			return rows[i].Key < rows[j].Key
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
}

// --- Value viewer ---------------------------------------------------

// ValueView is the value-viewer page's data.
type ValueView struct {
	DB, Namespace, Key string
	Found              bool

	TypeName    string
	NonPortable bool
	Masked      bool   // permanently redacted by --mask-keys; see MatchesMaskPattern
	Message     string // set instead of Rendered for non-portable/masked values
	Rendered    string
	IsJSON      bool

	SizeBytes    int64
	AccessCount  int64
	CreatedAt    time.Time
	LastAccessed time.Time
	TTLRemaining *time.Duration
	ExpiresAt    *time.Time // see KeyRow.ExpiresAt's doc comment
}

// TTLDisplay renders a snapshot-at-render-time TTL string ("-", "expired",
// or a duration), same convention as KeyRow.TTLDisplay.
func (v ValueView) TTLDisplay() string {
	if v.TTLRemaining == nil {
		return "-"
	}
	if *v.TTLRemaining <= 0 {
		return "expired"
	}
	return v.TTLRemaining.Round(time.Second).String()
}

// ExpiresAtRFC3339 renders ExpiresAt for the template's
// data-expires-at attribute, which static/countdown.js parses client-side.
func (v ValueView) ExpiresAtRFC3339() string {
	if v.ExpiresAt == nil {
		return ""
	}
	return v.ExpiresAt.Format(time.RFC3339)
}

// BuildValueView fetches metadata and, for portable, unmasked types, the
// decoded value for key. maskPatterns are --mask-keys glob patterns
// (nil/empty disables masking); a matching key's value is never fetched
// or decoded at all -- stronger than the default reveal-behind-a-click
// treatment every other value gets, since a masked value never reaches
// the client, revealable or not.
func BuildValueView(c *lytecache.Cache, db, namespace, key string, maskPatterns []string) (ValueView, error) {
	info, found, err := c.Inspect(key)
	if err != nil {
		return ValueView{}, err
	}
	v := ValueView{DB: db, Namespace: namespace, Key: key, Found: found}
	if !found {
		return v, nil
	}

	v.TypeName = typeCodeName(info.ValueType)
	v.SizeBytes = info.SizeBytes
	v.AccessCount = info.AccessCount
	v.CreatedAt = info.CreatedAt
	v.LastAccessed = info.LastAccessed
	if info.ExpiresAt != nil {
		d := time.Until(*info.ExpiresAt)
		v.TTLRemaining = &d
		v.ExpiresAt = info.ExpiresAt
	}

	if MatchesMaskPattern(key, maskPatterns) {
		v.Masked = true
		v.Message = "value redacted (matches a --mask-keys pattern)"
		return v, nil
	}

	if isNonPortable(info.ValueType) {
		v.NonPortable = true
		v.Message = nonPortableMessage(info.ValueType, info.SizeBytes)
		return v, nil
	}

	decoded, ok, err := getDecodedValue(c, key, info.ValueType)
	if err != nil {
		return ValueView{}, err
	}
	if !ok {
		// Deleted or expired between Inspect and Get -- a race, not an
		// error; report as not-found rather than a stale/blank value.
		v.Found = false
		return v, nil
	}

	rendered, err := renderValue(info.ValueType, decoded)
	if err != nil {
		return ValueView{}, err
	}
	v.Rendered = rendered
	v.IsJSON = info.ValueType == typeJSON
	return v, nil
}

// --- Cross-database search ---------------------------------------------------

// SearchResultRow is one match from a cross-database/namespace search.
type SearchResultRow struct {
	DB, Namespace, Key string
}

// SearchView is the search page's data.
type SearchView struct {
	Pattern   string
	Rows      []SearchResultRow
	Truncated bool
	Errors    []string
}

// Per-namespace and total caps bound the fanout cost of a cross-database
// search over many configured files/namespaces.
const (
	maxSearchPerNamespace = 200
	maxSearchTotal        = 500
)

// BuildSearch runs pattern (GLOB syntax) against every namespace of every
// configured, reachable database. A database or namespace that can't be
// read contributes an entry to Errors rather than aborting the search.
func BuildSearch(mgr *Manager, pattern string) SearchView {
	if pattern == "" {
		pattern = "*"
	}
	view := SearchView{Pattern: pattern}

	for _, name := range mgr.Names() {
		if view.Truncated {
			break
		}
		e, _ := mgr.entry(name)
		dc, err := e.Cache("default")
		if err != nil {
			view.Errors = append(view.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		nsInfos, err := dc.Namespaces()
		if err != nil {
			view.Errors = append(view.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		for _, nsInfo := range nsInfos {
			if searchNamespace(mgr, e, name, nsInfo.Namespace, pattern, &view) {
				break
			}
		}
	}
	return view
}

// searchNamespace appends matches from one namespace to view, returning
// true if the overall search should stop (total cap reached).
func searchNamespace(_ *Manager, e *dbEntry, dbName, namespace, pattern string, view *SearchView) bool {
	nc, err := e.Cache(namespace)
	if err != nil {
		view.Errors = append(view.Errors, fmt.Sprintf("%s/%s: %v", dbName, namespace, err))
		return false
	}
	count := 0
	for k, err := range nc.Keys(pattern) {
		if err != nil {
			view.Errors = append(view.Errors, fmt.Sprintf("%s/%s: %v", dbName, namespace, err))
			break
		}
		view.Rows = append(view.Rows, SearchResultRow{DB: dbName, Namespace: namespace, Key: k})
		count++
		if count >= maxSearchPerNamespace {
			view.Truncated = true
			return false
		}
		if len(view.Rows) >= maxSearchTotal {
			view.Truncated = true
			return true
		}
	}
	return false
}
