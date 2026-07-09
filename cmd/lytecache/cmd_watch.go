package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func newWatchCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "watch [interval]",
		Short: "Redraw stats every interval seconds (default 2) until Ctrl-C",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			interval := 2 * time.Second
			if len(args) > 0 {
				seconds, err := strconv.ParseFloat(args[0], 64)
				if err != nil {
					return usageErrorf("invalid interval %q: %v", args[0], err)
				}
				if seconds <= 0 {
					return usageErrorf("interval must be positive, got %s", args[0])
				}
				interval = time.Duration(seconds * float64(time.Second))
			}

			// In one-shot mode, every tick opens and closes its own Cache,
			// matching the open/act/close discipline described in the
			// README, so watch never holds a write connection longer than
			// a single Stats call, safely alongside a live application
			// using the file. In REPL mode, openCache instead reuses the
			// session's single already-open Cache each tick.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			w := cmd.OutOrStdout()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				c, closeFn, err := openCache(flags, true)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(w, "\033[H\033[2J") // clear screen
				_, _ = fmt.Fprintf(w, "lytecache watch -- every %s -- Ctrl-C to quit\n\n", interval)
				statsErr := runStats(w, c, false)
				closeFn()
				if statsErr != nil {
					return statsErr
				}

				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
				}
			}
		},
	}
}
