package ui

import (
	"crypto/subtle"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DefaultMetricsCacheTTL bounds how often a scrape actually reads the
// configured database files -- a tight scrape interval hammering every
// file on every poll would defeat the point of a read-only admin tool
// being a good citizen alongside the application it's inspecting.
const DefaultMetricsCacheTTL = 5 * time.Second

var (
	keysTotalDesc = prometheus.NewDesc(
		"lytecache_keys_total", "Number of keys in this namespace.",
		[]string{"database", "namespace"}, nil)
	sizeBytesDesc = prometheus.NewDesc(
		"lytecache_size_bytes", "Total size on disk of stored values in this namespace.",
		[]string{"database", "namespace"}, nil)
	expiredPresentDesc = prometheus.NewDesc(
		"lytecache_expired_present_total", "Keys past their expiry but not yet swept -- should stay near zero; a growing value indicates a sweeper defect.",
		[]string{"database", "namespace"}, nil)

	maxBytesDesc = prometheus.NewDesc(
		"lytecache_max_bytes", "Configured max_bytes capacity, when known (see --db name=path's declared limit, or the config file's databases: list). Absent, not zero, when unknown/unlimited.",
		[]string{"database"}, nil)
	maxKeysDesc = prometheus.NewDesc(
		"lytecache_max_keys", "Configured max_keys capacity, when known. Absent, not zero, when unknown/unlimited.",
		[]string{"database"}, nil)
	schemaVersionDesc = prometheus.NewDesc(
		"lytecache_schema_version", "On-disk schema_version this file was opened with.",
		[]string{"database"}, nil)
	fileReadableDesc = prometheus.NewDesc(
		"lytecache_file_readable", "1 if the database file opened successfully at this scrape, 0 otherwise.",
		[]string{"database"}, nil)

	// Gauges, not Counters, and deliberately so: lytecache-go's
	// Hits/Misses/Evictions/ExpiredRemoved are per-*Cache-instance
	// in-memory counters, never persisted to the file and never shared
	// across processes (see lytecache-go's Stats doc comment) -- a
	// Prometheus Counter implies monotonic-since-process-start semantics
	// that would be actively misleading here. These specific gauges
	// reflect only THIS UI's own Cache handle, not the application's --
	// hits_total/misses_total in particular will read 0 forever, since
	// the UI never serves application read traffic. They're exposed
	// anyway (per the spec's own "expose as gauges instead and document
	// the distinction plainly" guidance) because a maintenance pass run
	// from this UI does move evictions_total/expired_removed_total.
	hitsDesc = prometheus.NewDesc(
		"lytecache_hits_total", "Hits on THIS UI's own Cache handle only (per-process, not the application's -- expect 0; see docs/ui.md).",
		[]string{"database"}, nil)
	missesDesc = prometheus.NewDesc(
		"lytecache_misses_total", "Misses on THIS UI's own Cache handle only (per-process, not the application's -- expect 0; see docs/ui.md).",
		[]string{"database"}, nil)
	evictionsDesc = prometheus.NewDesc(
		"lytecache_evictions_total", "Evictions performed by THIS UI's own Cache handle (per-process; moves only if a maintenance pass is run from this UI).",
		[]string{"database"}, nil)
	expiredRemovedDesc = prometheus.NewDesc(
		"lytecache_expired_removed_total", "Expired keys removed by THIS UI's own Cache handle (per-process; moves only if a maintenance pass is run from this UI).",
		[]string{"database"}, nil)

	scrapeDurationDesc = prometheus.NewDesc(
		"lytecache_ui_scrape_duration_seconds", "Time spent computing this scrape's metrics.", nil, nil)
)

// metricsCollector implements prometheus.Collector by computing every
// metric fresh from the configured databases at scrape time (subject to
// cacheTTL), rather than maintaining running counters -- there's nothing
// to increment here, the file itself is the source of truth. Describe
// intentionally sends nothing (an "unchecked" collector, which
// prometheus.Collector's own doc comment sanctions): the metric set
// varies by how many databases/namespaces are configured, which isn't
// knowable up front.
type metricsCollector struct {
	mgr      *Manager
	cacheTTL time.Duration

	mu         sync.Mutex
	computedAt time.Time
	cached     []prometheus.Metric
}

func newMetricsCollector(mgr *Manager, cacheTTL time.Duration) *metricsCollector {
	if cacheTTL <= 0 {
		cacheTTL = DefaultMetricsCacheTTL
	}
	return &metricsCollector{mgr: mgr, cacheTTL: cacheTTL}
}

func (c *metricsCollector) Describe(_ chan<- *prometheus.Desc) {}

func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range c.metrics() {
		ch <- m
	}
}

func (c *metricsCollector) metrics() []prometheus.Metric {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && time.Since(c.computedAt) < c.cacheTTL {
		return c.cached
	}

	start := time.Now()
	var out []prometheus.Metric
	for _, name := range c.mgr.Names() {
		out = append(out, c.collectDatabase(name)...)
	}
	out = append(out, prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds()))

	c.cached = out
	c.computedAt = time.Now()
	return out
}

// collectDatabase reads exactly the zero-write methods added in Stage 0
// (Namespaces/SchemaVersion/Limits, plus Stats -- also a pure read, see
// its own doc comment) -- a scrape must never perturb the cache it's
// reading.
func (c *metricsCollector) collectDatabase(name string) []prometheus.Metric {
	e, ok := c.mgr.Entry(name)
	if !ok {
		return nil
	}
	dc, err := e.Cache("default")
	if err != nil {
		return []prometheus.Metric{
			prometheus.MustNewConstMetric(fileReadableDesc, prometheus.GaugeValue, 0, name),
		}
	}

	metrics := []prometheus.Metric{
		prometheus.MustNewConstMetric(fileReadableDesc, prometheus.GaugeValue, 1, name),
		prometheus.MustNewConstMetric(schemaVersionDesc, prometheus.GaugeValue, float64(dc.SchemaVersion()), name),
	}

	maxBytes := firstNonNil(e.DeclaredMaxBytes, dc.Limits().MaxBytes)
	maxKeys := firstNonNil(e.DeclaredMaxKeys, dc.Limits().MaxKeys)
	if maxBytes != nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(maxBytesDesc, prometheus.GaugeValue, float64(*maxBytes), name))
	}
	if maxKeys != nil {
		metrics = append(metrics, prometheus.MustNewConstMetric(maxKeysDesc, prometheus.GaugeValue, float64(*maxKeys), name))
	}

	if infos, err := dc.Namespaces(); err == nil {
		for _, info := range infos {
			metrics = append(metrics,
				prometheus.MustNewConstMetric(keysTotalDesc, prometheus.GaugeValue, float64(info.KeyCount), name, info.Namespace),
				prometheus.MustNewConstMetric(sizeBytesDesc, prometheus.GaugeValue, float64(info.SizeBytes), name, info.Namespace),
				prometheus.MustNewConstMetric(expiredPresentDesc, prometheus.GaugeValue, float64(info.ExpiredPresent), name, info.Namespace),
			)
		}
	}

	if stats, err := dc.Stats(); err == nil {
		metrics = append(metrics,
			prometheus.MustNewConstMetric(hitsDesc, prometheus.GaugeValue, float64(stats.Hits), name),
			prometheus.MustNewConstMetric(missesDesc, prometheus.GaugeValue, float64(stats.Misses), name),
			prometheus.MustNewConstMetric(evictionsDesc, prometheus.GaugeValue, float64(stats.Evictions), name),
			prometheus.MustNewConstMetric(expiredRemovedDesc, prometheus.GaugeValue, float64(stats.ExpiredRemoved), name),
		)
	}

	return metrics
}

// registerMetricsRoute mounts /metrics on its own local prometheus
// registry (never the global DefaultRegisterer -- a local registry per
// Server means multiple Servers can coexist in the same process, e.g.
// across tests, without a duplicate-registration panic) plus the standard
// Go/process collectors. A no-op if noMetrics is set: --no-metrics
// removes the route entirely, not just guards it.
func (s *Server) registerMetricsRoute(mux *http.ServeMux) {
	if s.noMetrics {
		return
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(newMetricsCollector(s.mgr, s.metricsCacheTTL))
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	s.handle(mux, "GET /metrics", s.requireMetricsAuth(handler))
}

// requireMetricsAuth enforces the bearer token when one is configured.
// /metrics is exempt from the session-cookie requirement everywhere else
// (see authExempt) -- a Prometheus scraper can't perform an interactive
// login -- but CheckMetricsGuardrail (auth.go) already ensures a token is
// mandatory whenever the server binds beyond loopback, so "no token
// configured" only ever means "bound to 127.0.0.1, scrape locally."
func (s *Server) requireMetricsAuth(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.metricsToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		want := "Bearer " + s.metricsToken
		got := r.Header.Get("Authorization")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}
