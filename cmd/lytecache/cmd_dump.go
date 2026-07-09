package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDumpCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "dump <key>",
		Short: "Print raw row metadata for a key -- the debugging view",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			key := args[0]
			info, found, err := c.Inspect(key)
			if err != nil {
				return databaseError(err)
			}
			if !found {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(nil)")
				return silentExit(exitFalseOrMiss)
			}

			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "key:            %s\n", key)
			_, _ = fmt.Fprintf(w, "value_type:     %d (%s)\n", info.ValueType, typeCodeName(info.ValueType))
			_, _ = fmt.Fprintf(w, "size_bytes:     %d\n", info.SizeBytes)
			_, _ = fmt.Fprintf(w, "created_at:     %s\n", info.CreatedAt.Local().Format("2006-01-02 15:04:05.000 MST"))
			_, _ = fmt.Fprintf(w, "last_accessed:  %s\n", info.LastAccessed.Local().Format("2006-01-02 15:04:05.000 MST"))
			_, _ = fmt.Fprintf(w, "access_count:   %d\n", info.AccessCount)
			if info.ExpiresAt != nil {
				_, _ = fmt.Fprintf(w, "expires_at:     %s\n", info.ExpiresAt.Local().Format("2006-01-02 15:04:05.000 MST"))
			} else {
				_, _ = fmt.Fprintf(w, "expires_at:     (none)\n")
			}
			if isNonPortable(info.ValueType) {
				_, _ = fmt.Fprintf(w, "value:          %s\n", nonPortableMessage(info.ValueType, info.SizeBytes))
			}
			return nil
		},
	}
}
