# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

Initial release. A `redis-cli`-style command-line tool for lytecache database files, built entirely on [`lytecache-go`](https://github.com/lytecache/lytecache-go)'s public API:

- One-shot commands: `get`/`set`/`del`/`exists`/`ttl`/`expire`/`persist`/`touch`/`incr`/`decr`/`keys` (alias `scan`)/`stats` (alias `info`)/`flush`/`maintain`/`vacuum`/`which`/`dump`/`watch`.
- Interactive REPL (bare `lytecache`) with line editing, history, case-insensitive command names, and Ctrl-C/Ctrl-D handling.
- Database resolution via `--db` / `LYTECACHE_PATH` / the library's default path.
- Exit codes (`0`/`1`/`2`/`3`) scripts can depend on; values to stdout, diagnostics to stderr.
- Value type handling: JSON pretty-printing, `--raw` for exact bytes, base64/`--file`/stdin for `--type bytes`, and a graceful `(non-portable value: ...)` message for Python-pickle/Java-serialized values this CLI cannot decode.
- Distribution: `go install`, Homebrew tap, Scoop bucket, winget manifest (attached to releases pending `microsoft/winget-pkgs` submission), `.deb`/`.rpm` packages, and a checksum-verifying `install.sh`.

Depends on `lytecache-go` v0.3.0+ (for `Cache.Inspect` and `Cache.Maintain` -- see that repo's changelog).
