package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	kservice "github.com/kardianos/service"
	"github.com/spf13/cobra"

	"github.com/lytecache/lytecache-cli/cmd/lytecache/service"
)

// newServiceCmd builds `lytecache service {install,uninstall,start,stop,
// restart,status,logs}` -- managing `lytecache ui` as an OS-level
// background service (launchd/systemd/Windows SCM, via
// github.com/kardianos/service; see package service's doc comment). This
// is entirely additive: it does not change any existing command, and
// `lytecache ui --port 7070` keeps working as a plain foreground process
// exactly as before -- these commands are optional, never required.
func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage lytecache ui as a background service",
		Long: "Install, start, stop, and inspect lytecache ui as a service managed by the\n" +
			"operating system (launchd on macOS, systemd on Linux, the Service Control\n" +
			"Manager on Windows) -- so it survives logout and restarts on boot, the same\n" +
			"experience as `brew services start redis` or `systemctl enable redis`.",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return checkNotRootOnDarwin(cmd)
		},
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceStartCmd(),
		newServiceStopCmd(),
		newServiceRestartCmd(),
		newServiceStatusCmd(),
		newServiceLogsCmd(),
	)
	return cmd
}

// checkNotRootOnDarwin rejects `lytecache service ...` under sudo/root on
// macOS before ever shelling out to launchctl. Unlike Linux (--system
// installs a real system-wide systemd unit that legitimately needs root),
// this tool has no system-daemon path on macOS at all -- describeInstallScope
// always reports "user LaunchAgent" there, no --system flag is even
// registered (see newServiceInstallCmd). Running as root against a
// LaunchAgent path produces a confusing raw launchctl error ("Expecting a
// LaunchDaemons path since the command was run as root. Got LaunchAgents
// instead.") instead of failing loudly with an actionable message, which
// the "fail loudly on bad permissions" design goal calls for.
func checkNotRootOnDarwin(cmd *cobra.Command) error {
	return checkNotRootOnGOOS(runtime.GOOS, os.Geteuid(), cmd.CommandPath())
}

// checkNotRootOnGOOS is checkNotRootOnDarwin's testable core -- goos/euid
// are parameters (rather than reading runtime.GOOS/os.Geteuid() directly)
// so the darwin-specific branch can be exercised by a unit test regardless
// of which OS actually runs `go test`, matching BuildConfig's
// GOOS-as-parameter pattern in config.go for the same reason.
func checkNotRootOnGOOS(goos string, euid int, cmdPath string) error {
	if goos != "darwin" || euid != 0 {
		return nil
	}
	return fmt.Errorf(
		"`lytecache %s` must not be run with sudo/as root on macOS -- "+
			"it manages a per-user LaunchAgent under ~/Library/LaunchAgents, "+
			"which launchd refuses to load when invoked as root "+
			"(there is no system-wide daemon mode on macOS); re-run this as your normal user",
		cmdPath[len("lytecache "):],
	)
}

func newServiceInstallCmd() *cobra.Command {
	var systemWide bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register lytecache ui with the OS service manager",
		Long: "Registers `lytecache ui` to start automatically (macOS: a user LaunchAgent;\n" +
			"Linux: a user systemd unit, or a system unit with --system; Windows: the\n" +
			"Service Control Manager if run elevated, otherwise a Scheduled Task at logon).\n" +
			"Every flag accepted by `lytecache ui` is accepted here too, and persisted so\n" +
			"the service starts identically every time.",
		Args: cobra.NoArgs,
	}
	v := bindUIFlags(cmd)
	if runtime.GOOS == "linux" {
		cmd.Flags().BoolVar(&systemWide, "system", false,
			"install a system-wide systemd unit (requires root) instead of the default user unit")
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		args := buildPersistedUIArgs(cmd, v)
		cfg := service.BuildConfig(args, systemWide)

		if runtime.GOOS == "windows" && !service.IsElevated() {
			if err := service.InstallScheduledTask(cfg.Name, args); err != nil {
				return databaseError(fmt.Errorf(
					"not running elevated, and the Scheduled Task fallback failed: %w -- "+
						"re-run from an elevated (Run as Administrator) shell to use the Service Control Manager instead", err))
			}
			if err := service.WriteInstallRecord(service.MethodScheduledTask); err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "installed as a Scheduled Task (not elevated -- run as Administrator to use the Service Control Manager instead)")
			return nil
		}

		svc, err := kservice.New(&service.Program{}, cfg)
		if err != nil {
			return databaseError(err)
		}
		if err := svc.Install(); err != nil {
			return databaseError(fmt.Errorf("installing service: %w", err))
		}
		if err := service.WriteInstallRecord(service.MethodOSService); err != nil {
			return databaseError(err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "installed %s (%s)\n", service.Name, describeInstallScope(systemWide))
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "start it with: lytecache service start")
		return nil
	}

	return cmd
}

func describeInstallScope(systemWide bool) string {
	switch runtime.GOOS {
	case "darwin":
		return "user LaunchAgent, ~/Library/LaunchAgents"
	case "windows":
		return "Service Control Manager"
	default:
		if systemWide {
			return "system systemd unit, /etc/systemd/system"
		}
		return "user systemd unit, ~/.config/systemd/user"
	}
}

// buildPersistedUIArgs reconstructs the `lytecache ui ...` argument list
// to persist into the service definition: only flags the operator
// actually passed to `service install` (cmd.Flags().Changed), plus every
// repeatable flag's full accumulated value, plus the hidden --as-service
// marker (see cmd_ui.go's newUICmd). Persisting only explicitly-set flags
// -- rather than every resolved value including defaults -- means a
// future lytecache upgrade's improved defaults apply on the next service
// restart instead of being frozen at whatever they were on install day.
func buildPersistedUIArgs(cmd *cobra.Command, v *uiFlagVars) []string {
	args := []string{"ui"}
	for _, db := range v.dbs {
		args = append(args, "--db", db.Name+"="+db.Path)
	}
	for _, dir := range v.scanDirs {
		args = append(args, "--scan", dir)
	}
	if cmd.Flags().Changed("port") {
		args = append(args, "--port", strconv.Itoa(v.port))
	}
	if cmd.Flags().Changed("host") {
		args = append(args, "--host", v.host)
	}
	if v.allowDelete {
		args = append(args, "--allow-delete")
	}
	if cmd.Flags().Changed("config") {
		args = append(args, "--config", v.configPath)
	}
	if cmd.Flags().Changed("tls-cert") {
		args = append(args, "--tls-cert", v.tlsCert)
	}
	if cmd.Flags().Changed("tls-key") {
		args = append(args, "--tls-key", v.tlsKey)
	}
	if v.insecure {
		args = append(args, "--insecure")
	}
	for _, pattern := range v.maskKeys {
		args = append(args, "--mask-keys", pattern)
	}
	if cmd.Flags().Changed("metrics-token") {
		args = append(args, "--metrics-token", v.metricsToken)
	}
	if v.noMetrics {
		args = append(args, "--no-metrics")
	}
	if cmd.Flags().Changed("metrics-cache-ttl") {
		args = append(args, "--metrics-cache-ttl", v.metricsCacheTTL.String())
	}
	args = append(args, "--as-service")
	return args
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove lytecache ui from the OS service manager",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			method, err := service.ReadInstallRecord()
			if err != nil {
				return databaseError(err)
			}
			if method == service.MethodScheduledTask {
				if err := service.UninstallScheduledTask(service.Name); err != nil {
					return databaseError(err)
				}
			} else {
				svc, err := kservice.New(&service.Program{}, service.BuildConfig(nil, false))
				if err != nil {
					return databaseError(err)
				}
				if err := svc.Uninstall(); err != nil {
					return databaseError(fmt.Errorf("uninstalling service: %w", err))
				}
			}
			if err := service.RemoveInstallRecord(); err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "uninstalled")
			return nil
		},
	}
}

func newServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the installed lytecache ui service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withInstalledService(cmd, func(svc kservice.Service) error { return svc.Start() },
				func() error { return service.StartScheduledTask(service.Name) })
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the installed lytecache ui service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withInstalledService(cmd, func(svc kservice.Service) error { return svc.Stop() },
				func() error { return service.StopScheduledTask(service.Name) })
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the installed lytecache ui service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withInstalledService(cmd, func(svc kservice.Service) error { return svc.Restart() },
				func() error {
					if err := service.StopScheduledTask(service.Name); err != nil {
						return err
					}
					return service.StartScheduledTask(service.Name)
				})
		},
	}
}

// withInstalledService dispatches to the OS-service or Scheduled-Task
// control path, whichever `service install` actually used (see
// install_record.go) -- kardianos/service has no idea a Scheduled Task
// exists, so the two paths can't share one call.
func withInstalledService(cmd *cobra.Command, viaOS func(kservice.Service) error, viaTask func() error) error {
	method, err := service.ReadInstallRecord()
	if err != nil {
		return databaseError(err)
	}
	if method == service.MethodScheduledTask {
		if err := viaTask(); err != nil {
			return databaseError(err)
		}
		return nil
	}
	svc, err := kservice.New(&service.Program{}, service.BuildConfig(nil, false))
	if err != nil {
		return databaseError(err)
	}
	if err := viaOS(svc); err != nil {
		return databaseError(err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ok")
	return nil
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether lytecache ui is running, its PID, uptime, bound address, and config/log paths",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			method, err := service.ReadInstallRecord()
			if err != nil {
				return databaseError(err)
			}

			running := false
			switch method {
			case service.MethodScheduledTask:
				running, err = service.ScheduledTaskStatus(service.Name)
				if err != nil {
					_, _ = fmt.Fprintf(out, "install method: scheduled task\nstatus:         unknown (%v)\n", err)
				}
			default:
				svc, svcErr := kservice.New(&service.Program{}, service.BuildConfig(nil, false))
				if svcErr != nil {
					return databaseError(svcErr)
				}
				st, statusErr := svc.Status()
				if statusErr != nil {
					_, _ = fmt.Fprintf(out, "install method: os service\nstatus:         not installed (%v)\n", statusErr)
					return nil
				}
				running = st == kservice.StatusRunning
			}

			_, _ = fmt.Fprintf(out, "install method: %s\n", method)
			if running {
				_, _ = fmt.Fprintln(out, "status:         running")
			} else {
				_, _ = fmt.Fprintln(out, "status:         stopped")
			}

			state, err := service.ReadState()
			switch {
			case err != nil:
				_, _ = fmt.Fprintln(out, "runtime info:   unavailable (no state file -- has it ever been started?)")
			case service.IsStale(state):
				_, _ = fmt.Fprintf(out, "runtime info:   stale (recorded PID %d is not running -- an unclean previous exit)\n", state.PID)
			default:
				_, _ = fmt.Fprintf(out, "pid:            %d\n", state.PID)
				_, _ = fmt.Fprintf(out, "uptime:         %s\n", time.Since(state.StartedAt).Round(time.Second))
				_, _ = fmt.Fprintf(out, "bound address:  %s\n", state.Addr)
				_, _ = fmt.Fprintf(out, "config:         %s\n", state.ConfigPath)
			}

			logPath, err := service.LogPath()
			if err == nil {
				_, _ = fmt.Fprintf(out, "log:            %s\n", logPath)
			}
			return nil
		},
	}
}

func newServiceLogsCmd() *cobra.Command {
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the tail of the lytecache ui service log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := service.LogPath()
			if err != nil {
				return databaseError(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return databaseError(fmt.Errorf("reading %s: %w", path, err))
			}
			tail := tailLines(data, lines)
			_, _ = cmd.OutOrStdout().Write(tail)
			return nil
		},
	}
	cmd.Flags().IntVar(&lines, "lines", 200, "number of trailing lines to show")
	return cmd
}

// tailLines returns the last n lines of data.
func tailLines(data []byte, n int) []byte {
	if n <= 0 {
		return data
	}
	count := 0
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			count++
			if count > n {
				return data[i+1:]
			}
		}
	}
	return data
}

// runUIUnderServiceManager is what `lytecache ui --as-service` actually
// runs (see newUICmd) -- it never runs interactively; the OS service
// manager invokes it with the arguments `service install` persisted. It
// hands the same buildUIServer/Serve/Shutdown construction runUI uses to
// kardianos/service's Program, so control (start/stop/restart) flows
// through the OS's own service lifecycle instead of a second, divergent
// implementation.
func runUIUnderServiceManager(_ *cobra.Command, opts uiRunOptions) error {
	logPath, err := service.LogPath()
	if err != nil {
		return databaseError(err)
	}
	logWriter, err := service.NewRotatingWriter(logPath, service.DefaultMaxLogBytes, service.DefaultMaxLogBackups)
	if err != nil {
		return databaseError(err)
	}
	defer func() { _ = logWriter.Close() }()

	var built *builtUIServer
	prog := &service.Program{
		StartFunc: func() error {
			b, err := buildUIServer(logWriter, logWriter, opts)
			if err != nil {
				return err
			}
			built = b
			if err := service.WriteState(service.State{
				PID: os.Getpid(), StartedAt: time.Now(), Addr: b.addr, ConfigPath: b.configPath,
			}); err != nil {
				_, _ = fmt.Fprintf(logWriter, "lytecache ui: warning: writing state file: %v\n", err)
			}
			go func() {
				if err := b.Serve(); err != nil {
					_, _ = fmt.Fprintf(logWriter, "lytecache ui: serve error: %v\n", err)
				}
			}()
			return nil
		},
		StopFunc: func(ctx context.Context) error {
			if built == nil {
				return nil
			}
			err := built.Shutdown(ctx)
			_ = service.RemoveState()
			return err
		},
	}

	svc, err := kservice.New(prog, service.BuildConfig(nil, false))
	if err != nil {
		return databaseError(err)
	}
	if err := svc.Run(); err != nil {
		return databaseError(err)
	}
	return nil
}

func newUIOpenCmd() *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the running lytecache ui in a browser",
		Long: "Opens http://<host>:<port> in the default browser. Defaults to the\n" +
			"127.0.0.1:7070 every `lytecache ui`/`service install` defaults to -- pass\n" +
			"--host/--port if this instance was configured differently.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := fmt.Sprintf("http://%s:%d", host, port)
			if err := openBrowser(url); err != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "could not open a browser automatically (%v) -- open this URL: %s\n", err, url)
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "opened %s\n", url)
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "host the running instance is bound to")
	cmd.Flags().IntVar(&port, "port", 7070, "port the running instance is bound to")
	return cmd
}
