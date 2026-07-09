package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func newTTLCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "ttl <key>",
		Short: "Show the remaining time-to-live for a key, in seconds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			ttl, hasExpiry, found, err := c.TTLOf(args[0])
			if err != nil {
				return databaseError(err)
			}
			if !found {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(nil)")
				return silentExit(exitFalseOrMiss)
			}
			if !hasExpiry {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), -1)
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), strconv.FormatFloat(ttl.Seconds(), 'f', -1, 64))
			return nil
		},
	}
}

// printBool prints "1"/"0" (matching exists' convention) and returns the
// silent exit-1 for false, so expire/persist/touch share one exit-code
// path.
func printBool(cmd *cobra.Command, ok bool) error {
	if ok {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), 1)
		return nil
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), 0)
	return silentExit(exitFalseOrMiss)
}

func newExpireCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "expire <key> <seconds>",
		Short: "Set or overwrite a key's TTL",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.ParseFloat(args[1], 64)
			if err != nil {
				return usageErrorf("invalid seconds %q: %v", args[1], err)
			}
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			ok, err := c.Expire(args[0], time.Duration(seconds*float64(time.Second)))
			if err != nil {
				return databaseError(err)
			}
			return printBool(cmd, ok)
		},
	}
}

func newPersistCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "persist <key>",
		Short: "Remove a key's TTL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			ok, err := c.Persist(args[0])
			if err != nil {
				return databaseError(err)
			}
			return printBool(cmd, ok)
		},
	}
}

func newTouchCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "touch <key> <seconds>",
		Short: "Refresh a key's TTL (sliding expiration)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.ParseFloat(args[1], 64)
			if err != nil {
				return usageErrorf("invalid seconds %q: %v", args[1], err)
			}
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			ok, err := c.Touch(args[0], time.Duration(seconds*float64(time.Second)))
			if err != nil {
				return databaseError(err)
			}
			return printBool(cmd, ok)
		},
	}
}
