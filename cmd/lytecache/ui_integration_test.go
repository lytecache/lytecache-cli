package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestUIIntegration compiles the real lytecache binary and drives `lytecache
// ui` as an actual OS process -- the one thing only the real os.Exit/signal
// path can catch, as opposed to the in-process runWithIO used by every
// other test in this package (see integration_test.go's identical
// rationale for the one-shot commands).
func TestUIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compiled-binary integration test in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT/SIGTERM graceful-shutdown semantics differ on windows; covered by service-manager tests in a later stage")
	}

	binPath := filepath.Join(t.TempDir(), "lytecache-ui-test-bin")
	build := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	dbPath := filepath.Join(t.TempDir(), "svc.db")
	seed := exec.Command(binPath, "--db", dbPath, "set", "greeting", "hello")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seeding fixture db: %v\n%s", err, out)
	}

	// --config isolates this run to a temp file -- without it, the server
	// would bootstrap (and this test would exercise) the real OS config
	// directory on whatever machine runs the test.
	configPath := filepath.Join(t.TempDir(), "ui.yaml")

	// --port 0 asks the OS for a free ephemeral port -- runUI logs the
	// actually-bound address (see cmd_ui.go), which this test scrapes
	// from stdout rather than guessing a port, avoiding any flakiness
	// from a fixed port being in use.
	cmd := exec.Command(binPath, "ui", "--port", "0", "--db", "svc="+dbPath, "--allow-delete", "--config", configPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	addr, err := readListeningAddr(stdout)
	if err != nil {
		t.Fatalf("reading listening address: %v\nstderr:\n%s", err, stderr.String())
	}

	// --- exercise the real HTTP server over the real network ---

	// A plain http.Client/PostForm follows redirects automatically, which
	// would hide the very status codes some assertions below care about,
	// and would silently drop the session cookie's Path scoping across
	// redirects in edge cases -- use one client throughout and manage the
	// cookie explicitly instead of relying on a cookie jar's redirect
	// behavior.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	loginForm := url.Values{"username": {"admin"}, "password": {"admin"}} // default first-run credentials
	loginResp, err := client.PostForm("http://"+addr+"/login", loginForm)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusFound {
		t.Fatalf("POST /login: status %d, want %d", loginResp.StatusCode, http.StatusFound)
	}
	var sessionCookie *http.Cookie
	for _, c := range loginResp.Cookies() {
		if c.Name == "lytecache_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set a session cookie")
	}

	get := func(path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(sessionCookie)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return resp
	}

	resp := get("/dashboard")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /dashboard: status %d, body:\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "svc") {
		t.Errorf("dashboard body missing configured database name \"svc\":\n%s", body)
	}

	resp = get("/db/svc/namespaces/default/keys/greeting")
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), "hello") {
		t.Errorf("key detail body missing the seeded value:\n%s", body)
	}

	// The value viewer page just rendered carries a CSRF token that isn't
	// exposed anywhere in this minimal (pre-Stage-3) template yet, so the
	// delete below is authenticated via a token derived the same way the
	// in-process ui-package tests do: log in again is unnecessary --
	// instead, extract nothing and rely on the server rejecting a
	// tokenless request, proving CSRF enforcement holds even against the
	// real compiled binary over a real socket.
	deleteReq, err := http.NewRequest(http.MethodPost, "http://"+addr+"/db/svc/namespaces/default/delete-key",
		strings.NewReader(url.Values{"key": {"greeting"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteReq.AddCookie(sessionCookie)
	deleteResp, err := client.Do(deleteReq)
	if err != nil {
		t.Fatalf("POST delete-key (no CSRF token): %v", err)
	}
	_ = deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusForbidden {
		t.Errorf("POST delete-key without a CSRF token: status %d, want %d", deleteResp.StatusCode, http.StatusForbidden)
	}

	// --- graceful shutdown on SIGINT, matching the manual smoke test ---

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Errorf("process did not exit cleanly after SIGINT: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s of SIGINT -- graceful shutdown likely hung")
	}
}

// readListeningAddr reads runUI's "lytecache ui listening on http://HOST:PORT"
// startup line and returns HOST:PORT. Startup now prints a few lines
// before it on first run (created config path, generated credentials), so
// this scans line by line instead of assuming the address is in the first
// read.
func readListeningAddr(stdout io.Reader) (string, error) {
	scanner := bufio.NewScanner(stdout)
	const prefix = "listening on http://"
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, prefix)
		if idx == -1 {
			continue
		}
		return strings.TrimSpace(line[idx+len(prefix):]), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("stdout closed before a %q line appeared", prefix)
}
