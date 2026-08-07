package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	lytecache "github.com/lytecache/lytecache-go"
)

func scrapeMetrics(t *testing.T, s *Server, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestMetricsIsValidPrometheusExpositionFormat(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	rec := scrapeMetrics(t, s, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200, body:\n%s", rec.Code, rec.Body.String())
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	if err != nil {
		t.Fatalf("response is not valid Prometheus text exposition format: %v\nbody:\n%s", err, rec.Body.String())
	}
	if len(families) == 0 {
		t.Fatal("expected at least one metric family")
	}
}

func TestMetricsHasPerDatabaseLabels(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	rec := scrapeMetrics(t, s, nil)
	body := rec.Body.String()

	for _, want := range []string{
		`lytecache_keys_total{database="svc",namespace="default"} 1`,
		`lytecache_file_readable{database="svc"} 1`,
		`lytecache_schema_version{database="svc"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected body to contain %q, got:\n%s", want, body)
		}
	}
}

func TestMetricsScrapeNeverMutatesTheCache(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set("k", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	before, found, err := c2(t, path).Inspect("k")
	if err != nil || !found {
		t.Fatalf("Inspect before scrape: found=%v err=%v", found, err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	scrapeMetrics(t, s, nil)

	after, found, err := c2(t, path).Inspect("k")
	if err != nil || !found {
		t.Fatalf("row disappeared after a scrape: found=%v err=%v", found, err)
	}
	if before.AccessCount != after.AccessCount || !before.LastAccessed.Equal(after.LastAccessed) {
		t.Errorf("scrape changed row state: before=%+v after=%+v", before, after)
	}
}

func TestMetricsValueCachePreventsRepeatedReads(t *testing.T) {
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServerWithConfig(t, Config{
		Databases:       []DBSource{{Name: "svc", Path: path}},
		MetricsCacheTTL: time.Hour, // long enough that a second scrape within the test can't miss the cache
	})

	first := scrapeMetrics(t, s, nil).Body.String()
	second := scrapeMetrics(t, s, nil).Body.String()

	// A recomputed scrape would report a freshly-measured (near-certainly
	// different) lytecache_ui_scrape_duration_seconds each time; two
	// scrapes within MetricsCacheTTL reporting the exact same duration
	// value is only possible if the second one served the cached
	// snapshot from the first, rather than reopening/re-querying the
	// database files again.
	scrapeDuration := func(body string) string {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "lytecache_ui_scrape_duration_seconds ") {
				return line
			}
		}
		return ""
	}
	d1, d2 := scrapeDuration(first), scrapeDuration(second)
	if d1 == "" || d2 == "" {
		t.Fatal("expected a lytecache_ui_scrape_duration_seconds sample in both scrapes")
	}
	if d1 != d2 {
		t.Errorf("scrape duration differed between two scrapes within the cache TTL, suggesting the second one recomputed: %q vs %q", d1, d2)
	}
}

func TestMetricsRequiresTokenWhenConfigured(t *testing.T) {
	s := newTestServerWithConfig(t, Config{MetricsToken: "s3cr3t"})

	rec := scrapeMetrics(t, s, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: code = %d, want 401", rec.Code)
	}

	rec = scrapeMetrics(t, s, map[string]string{"Authorization": "Bearer wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: code = %d, want 401", rec.Code)
	}

	rec = scrapeMetrics(t, s, map[string]string{"Authorization": "Bearer s3cr3t"})
	if rec.Code != http.StatusOK {
		t.Errorf("correct token: code = %d, want 200", rec.Code)
	}
}

func TestMetricsUnauthenticatedOnLoopbackWithNoToken(t *testing.T) {
	s := newTestServer(t) // no MetricsToken configured
	rec := scrapeMetrics(t, s, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d, want 200 (no token configured -> no auth required)", rec.Code)
	}
}

func TestNoMetricsRemovesTheRoute(t *testing.T) {
	s := newTestServerWithConfig(t, Config{NoMetrics: true})
	rec := scrapeMetrics(t, s, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 -- --no-metrics must remove the route, not just guard it", rec.Code)
	}
	for _, p := range s.RegisteredRoutes() {
		if p == "GET /metrics" {
			t.Error("GET /metrics must not be registered at all when NoMetrics is set")
		}
	}
}

func TestCheckMetricsGuardrail(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		noMetrics bool
		token     string
		wantErr   bool
	}{
		{"loopback, no token, fine", "127.0.0.1", false, "", false},
		{"non-loopback, no token, refused", "0.0.0.0", false, "", true},
		{"non-loopback, with token, fine", "0.0.0.0", false, "s3cr3t", false},
		{"non-loopback, no-metrics, fine", "0.0.0.0", true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckMetricsGuardrail(tc.host, tc.noMetrics, tc.token)
			if (err != nil) != tc.wantErr {
				t.Errorf("CheckMetricsGuardrail(%q, %v, %q) error = %v, wantErr %v", tc.host, tc.noMetrics, tc.token, err, tc.wantErr)
			}
		})
	}
}

func TestMetricsHitsMissesAreZeroFromUIsOwnHandle(t *testing.T) {
	// Documents (via a real assertion, not just a comment) the
	// per-process-counter limitation: the UI never generates application
	// read traffic through its own Cache handle, so hits/misses always
	// read 0 -- see metrics.go's doc comments on hitsDesc/missesDesc.
	path := tempDBPath(t)
	c, err := lytecache.New(lytecache.WithPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.GetString("k"); err != nil { // a miss, purely to prove even this doesn't leak into the metric below
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, DBSource{Name: "svc", Path: path})
	body := scrapeMetrics(t, s, nil).Body.String()
	if !strings.Contains(body, `lytecache_misses_total{database="svc"} 0`) {
		t.Errorf("expected lytecache_misses_total=0 (a miss on a DIFFERENT process's Cache handle must not appear here), got:\n%s", body)
	}
}
