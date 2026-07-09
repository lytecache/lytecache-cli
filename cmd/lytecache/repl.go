package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
)

// runREPL opens the database once for the whole interactive session (see
// openCache's REPL-mode note in db.go) and reads commands from a prompt
// until quit/exit/Ctrl-D.
func runREPL(flags *globalFlags) error {
	c, closeFn, err := openCache(flags, false)
	if err != nil {
		return err
	}
	defer closeFn()
	flags.sharedCache = c

	if !flags.quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Using database: %s\n", c.Path())
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            replPrompt(c.Path(), flags.namespace),
		HistoryFile:       historyFilePath(),
		InterruptPrompt:   "^C",
		EOFPrompt:         "quit",
		HistorySearchFold: true,
	})
	if err != nil {
		return databaseError(err)
	}
	defer func() { _ = rl.Close() }()

	for {
		line, err := rl.Readline()
		switch {
		case err == readline.ErrInterrupt:
			// Ctrl-C cancels the current line only; the REPL keeps going,
			// matching redis-cli and every other shell-like tool.
			continue
		case err == io.EOF:
			// Ctrl-D leaves.
			return nil
		case err != nil:
			return databaseError(err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		args, err := splitArgs(line)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		if len(args) == 0 {
			continue
		}

		// Command names are case-insensitive (Redis users type uppercase);
		// only the command name itself is lowercased, never key/value
		// arguments.
		args[0] = strings.ToLower(args[0])

		if args[0] == "quit" || args[0] == "exit" {
			return nil
		}

		runREPLCommand(flags, args)
	}
}

// runREPLCommand dispatches one already-tokenized REPL line through a
// fresh command tree built against the session's flags (so it reuses
// flags.sharedCache rather than opening a new connection -- see
// buildCommandTree and openCache), printing any error itself rather than
// stopping the REPL, since one bad command shouldn't end the session.
func runREPLCommand(flags *globalFlags, args []string) {
	cmd := buildCommandTree(flags)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		printREPLError(err)
	}
}

func printREPLError(err error) {
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.Err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "error:", ee.Err)
		}
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "error:", err)
}

// replPrompt builds the "lytecache (cache.db | ns: default)> " prompt,
// showing just the database file's base name (not its full path) to keep
// the prompt short.
func replPrompt(dbPath, namespace string) string {
	return fmt.Sprintf("lytecache (%s | ns: %s)> ", filepath.Base(dbPath), namespace)
}

// historyFilePath returns where the REPL's line-editing history is
// persisted across sessions, best-effort -- if the home directory can't be
// resolved, history is simply not persisted (readline still works fine
// in-session without a history file).
func historyFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".lytecache_history")
}

// splitArgs tokenizes a REPL line the way a shell would, for our purposes:
// whitespace-separated, with single or double quotes grouping a token that
// may itself contain whitespace -- needed for JSON values like
// {"name": "Ada"}, which a naive strings.Fields split would break apart.
// Backslash inside double quotes escapes a following quote or backslash;
// single-quoted text is taken completely literally.
func splitArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var hasCur bool
	inSingle, inDouble := false, false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(ch)
			}
		case inDouble:
			switch {
			case ch == '"':
				inDouble = false
			case ch == '\\' && i+1 < len(line) && (line[i+1] == '"' || line[i+1] == '\\'):
				i++
				cur.WriteByte(line[i])
			default:
				cur.WriteByte(ch)
			}
		case ch == '\'':
			inSingle, hasCur = true, true
		case ch == '"':
			inDouble, hasCur = true, true
		case ch == ' ' || ch == '\t':
			if hasCur {
				args = append(args, cur.String())
				cur.Reset()
				hasCur = false
			}
		default:
			cur.WriteByte(ch)
			hasCur = true
		}
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote")
	}
	if hasCur {
		args = append(args, cur.String())
	}
	return args, nil
}
