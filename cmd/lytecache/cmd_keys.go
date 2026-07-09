package main

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	lytecache "github.com/lytecache/lytecache-go"
)

func newKeysCmd(flags *globalFlags) *cobra.Command {
	var long bool
	cmd := &cobra.Command{
		Use:     "keys [pattern]",
		Aliases: []string{"scan"}, // same command under Redis-muscle-memory's other name
		Short:   "List keys matching a glob pattern (default *)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := "*"
			if len(args) > 0 {
				pattern = args[0]
			}
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()
			return runKeys(cmd.OutOrStdout(), c, pattern, long)
		},
	}
	cmd.Flags().BoolVar(&long, "long", false, "show type, ttl, and size columns")
	return cmd
}

func runKeys(w io.Writer, c *lytecache.Cache, pattern string, long bool) error {
	if !long {
		for key, err := range c.Keys(pattern) {
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(w, key)
		}
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KEY\tTYPE\tTTL\tSIZE")
	for key, err := range c.Keys(pattern) {
		if err != nil {
			_ = tw.Flush()
			return databaseError(err)
		}
		info, found, err := c.Inspect(key)
		if err != nil {
			_ = tw.Flush()
			return databaseError(err)
		}
		if !found {
			continue // expired in the gap between Keys and Inspect
		}
		ttlStr := "-"
		if info.ExpiresAt != nil {
			ttlStr = strconv.FormatFloat(time.Until(*info.ExpiresAt).Seconds(), 'f', 1, 64)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", key, typeCodeName(info.ValueType), ttlStr, info.SizeBytes)
	}
	return tw.Flush()
}
