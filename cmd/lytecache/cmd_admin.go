package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	lytecache "github.com/lytecache/lytecache-go"
)

// currentSchemaVersion mirrors the core library's schema_version constant
// (unexported there) purely for display in `stats`/`info`. This is a
// static wire-format constant, not behavior: a Cache can only ever open a
// file whose schema_version is <= this value in the first place (an
// incompatible file fails at open with ErrSchemaVersion, before any
// command runs), so there is no live value to query -- reporting it is
// just naming the constant every successfully-opened file already
// satisfies.
const currentSchemaVersion = 1

func newStatsCmd(flags *globalFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show cache statistics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()
			return runStats(cmd.OutOrStdout(), c, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	return cmd
}

// newInfoCmd is the same command under Redis's other common name.
func newInfoCmd(flags *globalFlags) *cobra.Command {
	cmd := newStatsCmd(flags)
	cmd.Use = "info"
	cmd.Short = "Alias for stats"
	return cmd
}

type statsView struct {
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
	HitRate        float64 `json:"hitRate"`
	KeyCount       int64   `json:"keyCount"`
	SizeBytes      int64   `json:"sizeBytes"`
	Evictions      int64   `json:"evictions"`
	ExpiredRemoved int64   `json:"expiredRemoved"`
	Path           string  `json:"path"`
	SchemaVersion  int     `json:"schemaVersion"`
}

func runStats(w io.Writer, c *lytecache.Cache, asJSON bool) error {
	s, err := c.Stats()
	if err != nil {
		return databaseError(err)
	}
	v := statsView{
		Hits:           s.Hits,
		Misses:         s.Misses,
		HitRate:        s.HitRate,
		KeyCount:       s.KeyCount,
		SizeBytes:      s.SizeBytes,
		Evictions:      s.Evictions,
		ExpiredRemoved: s.ExpiredRemoved,
		Path:           s.Path,
		SchemaVersion:  currentSchemaVersion,
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}

	_, _ = fmt.Fprintf(w, "keys:            %d\n", v.KeyCount)
	_, _ = fmt.Fprintf(w, "size:            %d bytes\n", v.SizeBytes)
	_, _ = fmt.Fprintf(w, "hits:            %d\n", v.Hits)
	_, _ = fmt.Fprintf(w, "misses:          %d\n", v.Misses)
	_, _ = fmt.Fprintf(w, "hit rate:        %.2f%%\n", v.HitRate*100)
	_, _ = fmt.Fprintf(w, "evictions:       %d\n", v.Evictions)
	_, _ = fmt.Fprintf(w, "expired removed: %d\n", v.ExpiredRemoved)
	_, _ = fmt.Fprintf(w, "schema version:  %d\n", v.SchemaVersion)
	_, _ = fmt.Fprintf(w, "path:            %s\n", v.Path)
	return nil
}

func newFlushCmd(flags *globalFlags) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "flush",
		Short: "Delete every key in the current namespace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout(),
					fmt.Sprintf("Delete all keys in namespace %q? [y/N] ", flags.namespace))
				if err != nil {
					return databaseError(err)
				}
				if !ok {
					return silentExit(exitFalseOrMiss)
				}
			}

			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := c.Flush(); err != nil {
				return databaseError(err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

// confirm prompts on w and reads a y/N answer from r. Only an explicit
// "y"/"yes" (case-insensitive) counts as confirmed; anything else,
// including an empty line, does not.
func confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprint(w, prompt)
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func newMaintainCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "maintain",
		Short: "Run one maintenance pass (expire sweep + eviction)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			result, err := c.Maintain()
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed %d expired key(s), evicted %d key(s)\n",
				result.ExpiredRemoved, result.Evicted)
			return nil
		},
	}
}

func newVacuumCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "vacuum",
		Short: "Reclaim disk space left behind by deleted rows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, closeFn, err := openCache(flags, true)
			if err != nil {
				return err
			}
			defer closeFn()

			before, err := fileSize(c.Path())
			if err != nil {
				return databaseError(err)
			}
			if err := c.Vacuum(); err != nil {
				return databaseError(err)
			}
			after, err := fileSize(c.Path())
			if err != nil {
				return databaseError(err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d bytes -> %d bytes\n", before, after)
			return nil
		},
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func newWhichCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "which",
		Short: "Print the resolved database path and whether it exists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := resolveDBPath(flags.db)
			if err != nil {
				return databaseError(err)
			}
			_, statErr := os.Stat(path)
			exists := statErr == nil
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), path)
			if exists {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(exists)")
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(does not exist yet)")
			return silentExit(exitFalseOrMiss)
		},
	}
}
