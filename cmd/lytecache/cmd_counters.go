package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

func newIncrCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "incr <key> [amount]",
		Short: "Atomically increment a counter (default amount: 1)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, err := parseCounterAmount(args)
			if err != nil {
				return usageErrorf("%v", err)
			}
			c, closeFn, err := openCache(flags, false)
			if err != nil {
				return err
			}
			defer closeFn()

			n, err := c.Incr(args[0], amount)
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), n)
			return nil
		},
	}
}

func newDecrCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "decr <key> [amount]",
		Short: "Atomically decrement a counter (default amount: 1)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			amount, err := parseCounterAmount(args)
			if err != nil {
				return usageErrorf("%v", err)
			}
			c, closeFn, err := openCache(flags, false)
			if err != nil {
				return err
			}
			defer closeFn()

			n, err := c.Decr(args[0], amount)
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), n)
			return nil
		},
	}
}

func parseCounterAmount(args []string) (int64, error) {
	if len(args) < 2 {
		return 1, nil
	}
	n, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", args[1], err)
	}
	return n, nil
}
