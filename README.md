# lytecache-cli

A command-line tool for inspecting and manipulating [lytecache](https://github.com/lytecache/lytecache-go) database files -- like `redis-cli`, but for a file instead of a server. It's built entirely on the [lytecache-go](https://github.com/lytecache/lytecache-go) library's public API (no duplicated cache logic), so anything a Go program can do to a cache file, this CLI can do from a shell or a script.

## Install

**macOS / Linux:**
```bash
brew install lytecache/tap/lytecache
# or, without Homebrew:
curl -fsSL https://raw.githubusercontent.com/lytecache/lytecache-cli/main/install.sh | sh
```

**Windows:**
```powershell
scoop bucket add lytecache https://github.com/lytecache/scoop-bucket
scoop install lytecache
```
A winget manifest is attached to each [GitHub release](https://github.com/lytecache/lytecache-cli/releases) as well, pending submission to `microsoft/winget-pkgs` (see [RELEASING.md](RELEASING.md)).

**Any platform, from source:**
```bash
go install github.com/lytecache/lytecache-cli/cmd/lytecache@latest
```

**Manual download:** grab the archive for your OS/arch from the [latest release](https://github.com/lytecache/lytecache-cli/releases/latest) -- each comes with a `checksums.txt`. Linux users can instead use the `.deb`/`.rpm` package from the same release.

## A session

```console
$ lytecache set user:42 '{"name":"Ada"}'
$ lytecache get user:42
{
  "name": "Ada"
}
$ lytecache incr hits
1
$ lytecache expire user:42 60
1
$ lytecache ttl user:42
59.991
$ lytecache keys 'user:*'
user:42
$ lytecache stats
keys:            2
size:            15 bytes
...
$ lytecache            # no args -> interactive REPL
lytecache (cache.db | ns: default)> get user:42
{
  "name": "Ada"
}
lytecache (cache.db | ns: default)> exit
```

Every command works identically whether you run it one-shot (as above, script-friendly) or type it at the REPL prompt -- the REPL is just a loop around the same command tree, sharing one already-open connection for the whole session instead of reopening the file per line. Command names are case-insensitive in the REPL (`GET`/`get` both work); Ctrl-C cancels the current line, Ctrl-D or `quit`/`exit` leaves.

## Commands

| Command | Description |
|---|---|
| `get <key>` | Print a value (JSON pretty-printed by default; `--raw` for exact bytes). `(nil)` + exit 1 on miss. |
| `set <key> [value] [--ttl seconds] [--type string\|int\|float\|json\|bytes] [--file <path>]` | Set a value. Type is inferred unless `--type` forces it; `--type bytes` reads base64, `--file`, or stdin (`-`). |
| `del <key>...` | Delete one or more keys; prints how many actually existed. |
| `exists <key>` | Prints `1`/`0`; exit code matches. |
| `ttl <key>` | Remaining seconds, `-1` for no expiry, or `(nil)`. |
| `expire <key> <seconds>` | Set or overwrite a key's TTL. |
| `persist <key>` | Remove a key's TTL. |
| `touch <key> <seconds>` | Refresh a key's TTL (sliding expiration). |
| `incr <key> [amount]` / `decr <key> [amount]` | Atomically adjust a counter (default amount: 1); prints the new value. |
| `keys [pattern]` (alias `scan`) | List keys matching a glob pattern (default `*`); `--long` adds type/ttl/size columns. |
| `stats` (alias `info`) | Key count, size on disk, hit/miss counters, evictions, schema version, path. `--json` for machine-readable output. |
| `flush [--yes]` | Delete every key in the current namespace (prompts unless `--yes`). |
| `maintain` | Run one maintenance pass (expire sweep + eviction); prints what it removed. |
| `vacuum` | Reclaim disk space; prints size before/after. |
| `which` | Print the resolved database path and whether it exists. |
| `dump <key>` | Raw row metadata: value type code/name, timestamps, sizes -- the debugging view. |
| `watch [interval]` | Redraw `stats` every `interval` seconds (default 2) until Ctrl-C. |

Global flags: `--db <path>`, `--namespace <name>` (default `default`), `--quiet` (suppress decoration), `--version`.

## Database resolution

Every command resolves which file to open in this order:

1. `--db <path>` flag
2. `LYTECACHE_PATH` environment variable
3. the library's own default (`lytecache.DefaultPath()` -- see [lytecache-go's README](https://github.com/lytecache/lytecache-go#where-is-my-data))

In interactive mode, the resolved path is printed to stderr on startup (and `lytecache which` reports it standalone in either mode). Read-only commands (`get`, `keys`, `stats`, ...) fail with the resolved path in the error message if the file doesn't exist; write commands (`set`, `incr`, ...) create it, matching the library's own behavior.

One-shot commands open the database, do exactly one thing, and close it again -- they never hold a connection open longer than necessary, so it's safe to run `lytecache get`/`keys`/`stats`/`dump` against a file a live application has open at the same time (WAL mode makes concurrent readers safe).

## Exit codes

Scripts can rely on these without parsing output:

| Code | Meaning |
|---|---|
| `0` | success |
| `1` | a read found nothing (`(nil)`), or a boolean result was false |
| `2` | usage error (bad arguments/flags) |
| `3` | database error |

```bash
if lytecache exists session:abc >/dev/null; then
    echo "still logged in"
fi
```

Values are always written to stdout and diagnostics to stderr, so `lytecache get key | jq .` works; `--quiet` suppresses banners/prompts/confirmations, and `NO_COLOR` is respected.

## Cross-language party trick

Since every lytecache implementation shares one on-disk format (see [SPEC.md](https://github.com/lytecache/lytecache-go/blob/main/SPEC.md)), this CLI can inspect a cache file written by a different language entirely. A Python process wrote this:

```python
cache.set("config", {"theme": "dark", "timeout": 30})
```

```console
$ lytecache --db ~/.cache/lytecache/abc123.db get config
{
  "theme": "dark",
  "timeout": 30
}
$ lytecache --db ~/.cache/lytecache/abc123.db dump config
key:            config
value_type:     4 (json)
size_bytes:     29
created_at:     2026-07-09 12:00:00.000 UTC
last_accessed:  2026-07-09 12:00:05.000 UTC
access_count:   1
expires_at:     (none)
```

No Python installation, no shared server, no export step -- just the same SQLite file on disk. Type codes 5 (Python pickle) and 6 (Java serialized) -- values this CLI cannot decode -- render as `(non-portable value: python-pickle, N bytes)` rather than erroring or dumping garbage.

## Relationship to lytecache-go

This repo contains only the CLI (`cmd/lytecache`) and depends on [`github.com/lytecache/lytecache-go`](https://github.com/lytecache/lytecache-go) like any other consumer -- it has no access to, and does not duplicate, any of that module's unexported cache logic. See that repo for the Go library itself, the on-disk format (`SPEC.md`), and language-agnostic background on lytecache.

## License

Apache License 2.0. See [LICENSE](LICENSE).
