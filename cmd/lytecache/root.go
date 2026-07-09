package main

import (
	"github.com/spf13/cobra"

	lytecache "github.com/lytecache/lytecache-go"
)

// version, commit, and date are set via -ldflags at build time (see
// .goreleaser.yaml). They default to these values for a plain `go build`/
// `go run`/`go install` without those flags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// globalFlags holds the persistent flags shared by every command.
type globalFlags struct {
	db        string
	namespace string
	quiet     bool

	// sharedCache is set only in REPL mode: the one Cache opened for the
	// whole interactive session, reused by every command line instead of
	// each command opening and closing its own (see openCache in db.go).
	sharedCache *lytecache.Cache
}

// buildCommandTree constructs the command tree against flags, without the
// top-level persistent flags or bare-invocation REPL launch (see
// newRootCmd for those). It is used both by the top-level CLI entry point
// and, once per input line, by the REPL to parse and dispatch a single
// command against the session's already-populated flags and shared Cache.
func buildCommandTree(flags *globalFlags) *cobra.Command {
	root := &cobra.Command{
		Use:   "lytecache",
		Short: "Inspect and manipulate lytecache database files",
		// This CLI controls its own error/usage printing and exit codes
		// (see exitcode.go and main.go) rather than cobra's defaults, so
		// scripts can rely on a specific exit code meaning a specific thing.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
	}

	root.AddCommand(
		newGetCmd(flags),
		newSetCmd(flags),
		newDelCmd(flags),
		newExistsCmd(flags),
		newTTLCmd(flags),
		newExpireCmd(flags),
		newPersistCmd(flags),
		newTouchCmd(flags),
		newIncrCmd(flags),
		newDecrCmd(flags),
		newKeysCmd(flags),
		newStatsCmd(flags),
		newInfoCmd(flags),
		newFlushCmd(flags),
		newMaintainCmd(flags),
		newVacuumCmd(flags),
		newWhichCmd(flags),
		newDumpCmd(flags),
		newWatchCmd(flags),
	)

	return root
}

// newRootCmd builds the top-level command used by main: buildCommandTree's
// tree, plus the persistent --db/--namespace/--quiet flags and the
// bare-invocation (no subcommand) REPL launch.
func newRootCmd() *cobra.Command {
	flags := &globalFlags{}
	root := buildCommandTree(flags)
	root.Long = "lytecache is a command-line tool for interacting with lytecache database files.\n\n" +
		"Run with no arguments to start an interactive session (a prompt, like redis-cli),\n" +
		"or pass a command to run it once and exit -- ideal for scripts."
	root.Version = buildVersionString()

	root.PersistentFlags().StringVar(&flags.db, "db", "", "database file path (overrides LYTECACHE_PATH and the default location)")
	root.PersistentFlags().StringVar(&flags.namespace, "namespace", "default", "logical partition within the database file")
	root.PersistentFlags().BoolVar(&flags.quiet, "quiet", false, "suppress decoration (banners, prompts, confirmations)")

	root.RunE = func(_ *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageErrorf("unknown command %q -- run 'lytecache --help' for usage", args[0])
		}
		return runREPL(flags)
	}

	return root
}

func buildVersionString() string {
	return version + " (commit " + commit + ", built " + date + ")"
}
