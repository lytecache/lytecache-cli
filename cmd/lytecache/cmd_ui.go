package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/lytecache/lytecache-cli/cmd/lytecache/ui"
)

// uiRunOptions is everything buildUIServer needs. uiFlagVars binds cobra
// flags directly into one of these (via pointer fields), so both
// `lytecache ui` and `lytecache service install` (which needs the exact
// same flag set, to persist for the OS-managed daemon -- see
// cmd_service.go) can share one flag-registration function instead of
// declaring these ~12 flags twice.
type uiRunOptions struct {
	port            int
	host            string
	dbs             []ui.DBSource
	scanDirs        []string
	allowDelete     bool
	configPath      string
	tlsCert         string
	tlsKey          string
	insecure        bool
	maskKeys        []string
	metricsToken    string
	noMetrics       bool
	metricsCacheTTL time.Duration
}

type uiFlagVars struct {
	port            int
	host            string
	dbs             []ui.DBSource
	scanDirs        []string
	allowDelete     bool
	configPath      string
	tlsCert         string
	tlsKey          string
	insecure        bool
	maskKeys        []string
	metricsToken    string
	noMetrics       bool
	metricsCacheTTL time.Duration
}

func (v *uiFlagVars) toOptions() uiRunOptions {
	return uiRunOptions{
		port: v.port, host: v.host, dbs: v.dbs, scanDirs: v.scanDirs, allowDelete: v.allowDelete,
		configPath: v.configPath, tlsCert: v.tlsCert, tlsKey: v.tlsKey, insecure: v.insecure,
		maskKeys: v.maskKeys, metricsToken: v.metricsToken, noMetrics: v.noMetrics,
		metricsCacheTTL: v.metricsCacheTTL,
	}
}

// bindUIFlags registers the flags shared between `lytecache ui` and
// `lytecache service install` onto cmd.
//
// --db shadows the persistent, single-path --db flag every other command
// inherits from root (see newRootCmd in root.go): `ui` manages several
// database files at once, not one, so its --db is repeatable and takes
// name=path rather than a bare path. A local flag of the same name on a
// cobra subcommand takes precedence over an inherited persistent one of
// that name, which is what makes this safe -- see MergeSources' doc
// comment for how this combines with --scan and the config file's
// `databases:` list.
func bindUIFlags(cmd *cobra.Command) *uiFlagVars {
	v := &uiFlagVars{}
	cmd.Flags().Var(newDBSourceFlagValue(&v.dbs), "db", "database file as name=/path/to.db (repeatable)")
	cmd.Flags().StringArrayVar(&v.scanDirs, "scan", nil, "directory to auto-discover *.db files from (repeatable)")
	cmd.Flags().IntVar(&v.port, "port", 7070, "port to listen on")
	cmd.Flags().StringVar(&v.host, "host", "127.0.0.1",
		"address to bind -- binding beyond 127.0.0.1 requires further guardrails, see docs/ui.md")
	cmd.Flags().BoolVar(&v.allowDelete, "allow-delete", false,
		"enable delete/flush/maintain (vacuum is always allowed); without this the UI is strictly read-only")
	cmd.Flags().StringVar(&v.configPath, "config", "", "path to ui.yaml (default: OS config dir, see docs/ui.md)")
	cmd.Flags().StringVar(&v.tlsCert, "tls-cert", "", "TLS certificate file (required to bind beyond 127.0.0.1, unless --insecure)")
	cmd.Flags().StringVar(&v.tlsKey, "tls-key", "", "TLS private key file")
	cmd.Flags().BoolVar(&v.insecure, "insecure", false,
		"acknowledge serving beyond 127.0.0.1 over plain HTTP without TLS -- traffic, including the session cookie, is not encrypted")
	cmd.Flags().StringArrayVar(&v.maskKeys, "mask-keys", nil,
		"glob pattern (repeatable): matching keys' values are never shown in the value viewer, e.g. --mask-keys '*otp*'")
	cmd.Flags().StringVar(&v.metricsToken, "metrics-token", "",
		"bearer token required on /metrics -- mandatory when --host is not 127.0.0.1")
	cmd.Flags().BoolVar(&v.noMetrics, "no-metrics", false, "disable the /metrics endpoint entirely")
	cmd.Flags().DurationVar(&v.metricsCacheTTL, "metrics-cache-ttl", ui.DefaultMetricsCacheTTL,
		"how long a /metrics scrape's computed values are cached before re-reading the database files")
	return v
}

// newUICmd builds `lytecache ui`: a local web administration server, not a
// cache server -- see package ui's doc comment. This is additive to the
// existing command tree (see buildCommandTree in root.go); it does not
// change the behavior of get/set/del or any other existing command.
func newUICmd(flags *globalFlags) *cobra.Command {
	var asService bool

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the web-based administration UI",
		Long: "Start a local web UI for inspecting and cleaning up lytecache database files\n" +
			"(a RedisInsight/pgAdmin-style tool, not a cache server -- no application ever\n" +
			"connects to it). Binds 127.0.0.1 by default; see docs/ui.md before passing --host.",
		Args: cobra.NoArgs,
	}
	v := bindUIFlags(cmd)
	cmd.Flags().BoolVar(&asService, "as-service", false, "internal: run under the OS service manager")
	_ = cmd.Flags().MarkHidden("as-service")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		// buildCommandTree is reused, once per input line, by the REPL
		// (see repl.go) -- every other command is a fast, one-shot
		// action that fits that model. `ui` is a long-running server:
		// running it from inside the REPL would just hang the
		// interactive prompt until Ctrl-C, with no way to keep using
		// the session, so it's refused outright rather than allowed
		// to silently misbehave.
		if flags.sharedCache != nil {
			return usageErrorf("ui cannot be run from inside the REPL -- run 'lytecache ui' directly from a shell")
		}
		opts := v.toOptions()
		if asService {
			// Set only by the arguments `service install` persists into
			// the OS service manager -- see cmd_service.go and
			// service/program.go. Not documented for interactive use
			// (hidden above): running it by hand just re-blocks in
			// svc.Run() the same way runUI would block anyway.
			return runUIUnderServiceManager(cmd, opts)
		}
		return runUI(cmd, opts)
	}

	cmd.AddCommand(newUIPasswdCmd(), newUIResetPasswordCmd(), newUIOpenCmd())

	return cmd
}

// builtUIServer is the constructed-but-not-yet-serving state shared by both
// the foreground `lytecache ui` path (runUI) and the OS-service-managed
// path (service/program.go's Start/Stop) -- one code path builds the
// server; only how its lifecycle is driven (a blocking select on an OS
// signal vs. kardianos/service's Start/Stop callbacks) differs.
type builtUIServer struct {
	httpServer *http.Server
	listener   net.Listener
	uiServer   *ui.Server
	addr       string
	configPath string

	serveTLSCert string
	serveTLSKey  string
}

// buildUIServer loads/bootstraps the auth config, enforces the exposure
// guardrails, builds the ui.Server, and starts listening -- everything
// short of actually serving requests or blocking.
func buildUIServer(logOut, logErr io.Writer, opts uiRunOptions) (*builtUIServer, error) {
	configPath, err := resolveUIConfigPath(opts.configPath)
	if err != nil {
		return nil, err
	}

	authCfg, created, err := ui.LoadOrCreateAuthConfig(configPath)
	if err != nil {
		return nil, err
	}
	if created {
		_, _ = fmt.Fprintf(logOut, "first run: created %s\n", configPath)
		_, _ = fmt.Fprintf(logOut, "generated admin credentials -- username: %s  password: %s\n", authCfg.Username, ui.DefaultPassword)
		_, _ = fmt.Fprintln(logOut, "change this with 'lytecache ui passwd', especially before ever binding beyond 127.0.0.1")
	}

	tlsConfigured := opts.tlsCert != "" && opts.tlsKey != ""
	if err := ui.CheckStartupGuardrails(opts.host, authCfg, tlsConfigured, opts.insecure); err != nil {
		return nil, err
	}
	nonLoopback := !ui.IsLoopbackHost(opts.host)
	if nonLoopback && opts.insecure && !tlsConfigured {
		_, _ = fmt.Fprintln(logErr,
			"WARNING: --insecure -- serving the admin UI over plain HTTP on a non-loopback address. "+
				"Traffic, including the session cookie, is not encrypted. Prefer an SSH tunnel (see docs/ui.md) or --tls-cert/--tls-key.")
	}
	if err := ui.CheckMetricsGuardrail(opts.host, opts.noMetrics, opts.metricsToken); err != nil {
		return nil, err
	}

	// Config-file `databases:` first, then --db flags, matching the
	// documented merge order (see MergeSources's doc comment) --
	// NewServer's MergeSources call expects this ordering already applied.
	databases := make([]ui.DBSource, 0, len(authCfg.Databases)+len(opts.dbs))
	databases = append(databases, authCfg.Databases...)
	databases = append(databases, opts.dbs...)

	scanDirs := opts.scanDirs
	if cacheDir, err := os.UserCacheDir(); err == nil {
		scanDirs = defaultScanDirsIfUnconfigured(databases, scanDirs, cacheDir)
	}

	// Next to whichever config file is actually in use -- not always
	// ui.DefaultAuditLogPath(), since --config may have overridden it.
	auditPath := filepath.Join(filepath.Dir(configPath), "audit.log")

	srv, err := ui.NewServer(ui.Config{
		Databases:   databases,
		ScanDirs:    scanDirs,
		AllowDelete: opts.allowDelete,
		Logf: func(format string, args ...any) {
			_, _ = fmt.Fprintf(logErr, format+"\n", args...)
		},
		AuthConfig:      authCfg,
		AuthConfigPath:  configPath,
		NonLoopbackBind: nonLoopback,
		SecureCookie:    tlsConfigured,
		AuditLogPath:    auditPath,
		MaskKeys:        opts.maskKeys,
		NoMetrics:       opts.noMetrics,
		MetricsToken:    opts.metricsToken,
		MetricsCacheTTL: opts.metricsCacheTTL,
	})
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s:%d", opts.host, opts.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = srv.Close()
		return nil, portError(addr, err)
	}

	httpServer := &http.Server{Handler: srv.Handler()}
	built := &builtUIServer{httpServer: httpServer, listener: ln, uiServer: srv, addr: ln.Addr().String(), configPath: configPath}

	scheme := "http"
	if tlsConfigured {
		scheme = "https"
	}
	_, _ = fmt.Fprintf(logOut, "lytecache ui listening on %s://%s\n", scheme, built.addr)
	if !opts.allowDelete {
		_, _ = fmt.Fprintln(logOut, "read-only (pass --allow-delete to enable delete/flush/maintain)")
	}

	built.serveTLSCert, built.serveTLSKey = opts.tlsCert, opts.tlsKey
	return built, nil
}

// dockerSharedCacheDir is this project's own documented convention for
// where a lytecache Docker deployment's shared cache volume gets mounted
// -- see README.md's "Docker" section, examples/docker-compose.yml, and
// Dockerfile.cli, which all consistently use
// LYTECACHE_PATH=/var/cache/lytecache/cache.db and mount a named volume
// at this exact directory. Scanning it by default is the container-world
// analog of scanning <platform cache dir>/lytecache on a bare host: not
// arbitrary discovery, just not making the operator spell out a location
// this project itself already treats as the standard one. Harmless to
// include unconditionally on a non-container host too, since it simply
// won't exist there and filepath.Glob (see MergeSources) finds no
// matches in a nonexistent directory.
const dockerSharedCacheDir = "/var/cache/lytecache"

// defaultScanDirsIfUnconfigured falls back to scanning the standard,
// well-known locations -- "<platform cache dir>/lytecache" (the directory
// lytecache.DefaultPath derives its own default file from, in every
// library implementation) and dockerSharedCacheDir (this project's own
// documented Docker volume-mount convention) -- but only when the
// operator gave zero configuration at all (no --db, no --scan, nothing in
// the config file's databases: list). This is not silent auto-discovery
// of arbitrary files; it's the operator not having to spell out a
// location the project itself already treats as standard. Any explicit
// --db/--scan/config-file entry fully overrides this. A nonexistent
// directory (e.g. a first run before any app has ever written a cache
// file yet, or a bare host where the Docker convention path just doesn't
// exist) is fine -- filepath.Glob (see MergeSources) just finds no
// matches there.
//
// cacheDir is a parameter (rather than calling os.UserCacheDir directly)
// so this is unit-testable without depending on the host's actual cache
// directory, matching this file's BuildConfig/checkNotRootOnGOOS pattern.
func defaultScanDirsIfUnconfigured(databases []ui.DBSource, scanDirs []string, cacheDir string) []string {
	if len(databases) != 0 || len(scanDirs) != 0 {
		return scanDirs
	}
	return []string{filepath.Join(cacheDir, "lytecache"), dockerSharedCacheDir}
}

// portError distinguishes "the port is already in use" from other listen
// failures with a clearer message -- both for the foreground path and for
// service/program.go, where the OS service manager's restart policy
// should not mask a misconfiguration like this by endlessly retrying.
func portError(addr string, err error) error {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("port already in use: %s -- stop whatever else is bound there, or pass a different --port", addr)
	}
	return fmt.Errorf("listening on %s: %w", addr, err)
}

// Serve blocks, serving requests until Shutdown is called (or a fatal
// error occurs). Run this in a goroutine when a non-blocking start is
// needed (service/program.go's Start).
func (b *builtUIServer) Serve() error {
	var err error
	if b.serveTLSCert != "" {
		err = b.httpServer.ServeTLS(b.listener, b.serveTLSCert, b.serveTLSKey)
	} else {
		err = b.httpServer.Serve(b.listener)
	}
	if err != nil && errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the HTTP server and closes every database
// this server opened.
func (b *builtUIServer) Shutdown(ctx context.Context) error {
	var errs []error
	if err := b.httpServer.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := b.uiServer.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// runUI is the foreground `lytecache ui` path: build, serve, and block
// until an interrupt/TERM signal triggers a graceful shutdown.
func runUI(cmd *cobra.Command, opts uiRunOptions) error {
	built, err := buildUIServer(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts)
	if err != nil {
		return databaseError(err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- built.Serve() }()

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := built.Shutdown(shutdownCtx); err != nil {
			return databaseError(err)
		}
		return nil
	case err := <-serveErr:
		_ = built.uiServer.Close()
		if err != nil {
			return databaseError(err)
		}
		return nil
	}
}

func resolveUIConfigPath(override string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	return ui.DefaultConfigPath()
}

func newUIPasswdCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "passwd",
		Short: "Change the ui admin password from the terminal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveUIConfigPath(configPath)
			if err != nil {
				return databaseError(err)
			}
			cfg, _, err := ui.LoadOrCreateAuthConfig(path)
			if err != nil {
				return databaseError(err)
			}

			password, err := readPassword(cmd, "New password: ")
			if err != nil {
				return databaseError(err)
			}
			if len(password) < 8 {
				return usageErrorf("password must be at least 8 characters")
			}
			confirmed, err := readPassword(cmd, "Confirm password: ")
			if err != nil {
				return databaseError(err)
			}
			if password != confirmed {
				return usageErrorf("passwords did not match")
			}

			hash, err := ui.HashPassword(password)
			if err != nil {
				return databaseError(err)
			}
			cfg.PasswordHash = hash
			if err := ui.SaveAuthConfig(path, cfg); err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "password updated")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to ui.yaml (default: OS config dir)")
	return cmd
}

func newUIResetPasswordCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Restore the default admin/admin credentials for a locked-out operator",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveUIConfigPath(configPath)
			if err != nil {
				return databaseError(err)
			}
			cfg, _, err := ui.LoadOrCreateAuthConfig(path)
			if err != nil {
				return databaseError(err)
			}

			hash, err := ui.HashPassword(ui.DefaultPassword)
			if err != nil {
				return databaseError(err)
			}
			cfg.Username = ui.DefaultUsername
			cfg.PasswordHash = hash
			if err := ui.SaveAuthConfig(path, cfg); err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "credentials reset to %s/%s -- both exposure guardrails are re-armed until the password is changed again\n",
				ui.DefaultUsername, ui.DefaultPassword)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to ui.yaml (default: OS config dir)")
	return cmd
}

// readPassword prompts on cmd's stdout and reads a password from cmd's
// stdin, hiding input when stdin is a real terminal and falling back to a
// plain line read otherwise (piped input, e.g. in tests or scripted
// provisioning).
func readPassword(cmd *cobra.Command, prompt string) (string, error) {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), prompt)
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
