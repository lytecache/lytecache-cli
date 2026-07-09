package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	lytecache "github.com/lytecache/lytecache-go"
)

func newGetCmd(flags *globalFlags) *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get the value of a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()
			return runGet(cmd, c, args[0], raw)
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "print the exact stored bytes instead of pretty-printing")
	return cmd
}

func runGet(cmd *cobra.Command, c *lytecache.Cache, key string, raw bool) error {
	info, found, err := c.Inspect(key)
	if err != nil {
		return databaseError(err)
	}
	if !found {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(nil)")
		return silentExit(exitFalseOrMiss)
	}
	if isNonPortable(info.ValueType) {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), nonPortableMessage(info.ValueType, info.SizeBytes))
		return nil
	}

	value, found, err := getDecoded(c, key, info.ValueType)
	if err != nil {
		return databaseError(err)
	}
	if !found {
		// Expired in the gap between Inspect and the decode above.
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(nil)")
		return silentExit(exitFalseOrMiss)
	}

	if info.ValueType == typeBytes && raw {
		_, err := cmd.OutOrStdout().Write(value.([]byte))
		return err
	}

	formatted, err := formatDecodedValue(info.ValueType, value, raw)
	if err != nil {
		return databaseError(err)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatted)
	return nil
}

func newSetCmd(flags *globalFlags) *cobra.Command {
	var ttlSeconds float64
	var typeFlag string
	var fileFlag string

	cmd := &cobra.Command{
		Use:   "set <key> [value]",
		Short: "Set the value of a key",
		Long: "Set the value of a key.\n\n" +
			"Without --type, the value is inferred: a valid integer literal becomes an\n" +
			"int, a valid float literal becomes a float, valid JSON (an object, array,\n" +
			"bool, null, or quoted string) becomes its decoded form, and anything else\n" +
			"is stored as a plain string.\n\n" +
			"--type bytes reads the value as base64-encoded text, or raw bytes from\n" +
			"--file <path> or stdin (pass \"-\" as the value to read stdin).",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			hasValue := len(args) > 1
			var rawValue string
			if hasValue {
				rawValue = args[1]
			}
			if typeFlag != "" && typeFlag != "string" && typeFlag != "int" && typeFlag != "float" &&
				typeFlag != "json" && typeFlag != "bytes" {
				return usageErrorf("unknown --type %q: must be one of string, int, float, json, bytes", typeFlag)
			}

			value, err := parseSetValue(rawValue, hasValue, typeFlag, fileFlag, cmd.InOrStdin())
			if err != nil {
				return usageErrorf("%v", err)
			}

			c, closeFn, err := openCache(flags, false)
			if err != nil {
				return err
			}
			defer closeFn()

			var opts []lytecache.SetOption
			if cmd.Flags().Changed("ttl") {
				opts = append(opts, lytecache.TTL(time.Duration(ttlSeconds*float64(time.Second))))
			}
			if err := c.Set(key, value, opts...); err != nil {
				return databaseError(err)
			}
			return nil
		},
	}
	cmd.Flags().Float64Var(&ttlSeconds, "ttl", 0, "expire after this many seconds")
	cmd.Flags().StringVar(&typeFlag, "type", "", "force the value type: string|int|float|json|bytes")
	cmd.Flags().StringVar(&fileFlag, "file", "", "read a --type bytes value from this file")
	return cmd
}

func newDelCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "del <key>...",
		Aliases: []string{"delete"},
		Short:   "Delete one or more keys",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			n, err := c.Delete(args...)
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), n)
			return nil
		},
	}
}

func newExistsCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "exists <key>",
		Short: "Check whether a key exists",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			ok, err := c.Exists(args[0])
			if err != nil {
				return databaseError(err)
			}
			if ok {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), 1)
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), 0)
			return silentExit(exitFalseOrMiss)
		},
	}
}
