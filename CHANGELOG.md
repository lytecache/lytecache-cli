# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

- `lytecache ui`: a local web administration console (RedisInsight/pgAdmin-style, not a cache
  server -- no wire protocol, nothing an application connects to). Fleet dashboard across every
  configured database, a namespace-aware key browser with glob/sort/pagination, a value viewer
  handling every type code including the non-portable-language message, and cross-database search.
  Delete-only: single/bulk key delete, typed-confirmation namespace flush, a maintenance pass, and
  vacuum -- all gated behind `--allow-delete` (vacuum excepted, since it deletes no rows); there is
  no route, anywhere, that can create or alter a value.
  - Authentication: `ui.yaml` (argon2id-hashed password, session secret, database list), default
    `admin`/`admin` on first run, `lytecache ui passwd`/`reset-password`, session cookies, CSRF,
    per-IP rate limiting. Binding beyond `127.0.0.1` with the default password refuses to start
    outright; `--mask-keys` permanently redacts matching keys; every write is appended to an audit
    log (key names only, never values).
  - `/metrics`: standard Prometheus text exposition via `prometheus/client_golang`, gauges labeled
    by database/namespace, a 5s (configurable) computed-value cache so scraping never hammers the
    files, bearer-token auth mandatory once bound beyond loopback.
  - `lytecache service {install,uninstall,start,stop,restart,status,logs}`: runs `ui` as a managed
    background service via `kardianos/service` (a user LaunchAgent on macOS, a user or `--system`
    systemd unit on Linux, the Service Control Manager on Windows with a Scheduled Task fallback
    when not elevated) -- the exact same server-startup code path as the foreground command, which
    keeps working unchanged. `lytecache ui open` opens it in a browser.
  - See [docs/ui.md](docs/ui.md) for the full guide: multi-database configuration, the SSH-tunnel
    recommendation, exposure guardrails, and per-OS service instructions.

Depends on `lytecache-go` v0.3.0+ (for `Cache.Namespaces`/`SchemaVersion`/`Limits` and
`Stats.ExpiredPresent` -- see that repo's changelog).

## [0.1.0] - 2026-08-05

Initial release. A `redis-cli`-style command-line tool for lytecache database files, built entirely on [`lytecache-go`](https://github.com/lytecache/lytecache-go)'s public API:

- One-shot commands: `get`/`set`/`del`/`exists`/`ttl`/`expire`/`persist`/`touch`/`incr`/`decr`/`keys` (alias `scan`)/`stats` (alias `info`)/`flush`/`maintain`/`vacuum`/`which`/`dump`/`watch`.
- Interactive REPL (bare `lytecache`) with line editing, history, case-insensitive command names, and Ctrl-C/Ctrl-D handling.
- Database resolution via `--db` / `LYTECACHE_PATH` / the library's default path.
- Exit codes (`0`/`1`/`2`/`3`) scripts can depend on; values to stdout, diagnostics to stderr.
- Value type handling: JSON pretty-printing, `--raw` for exact bytes, base64/`--file`/stdin for `--type bytes`, and a graceful `(non-portable value: ...)` message for Python-pickle/Java-serialized values this CLI cannot decode.
- Distribution: `go install`, Homebrew tap, Scoop bucket, winget manifest (attached to releases pending `microsoft/winget-pkgs` submission), `.deb`/`.rpm` packages, and a checksum-verifying `install.sh`.
- A multi-arch (`linux/amd64`/`linux/arm64`) Docker image at `ghcr.io/lytecache/lytecache` (~7 MB, `scratch`-based), plus a `distroless`-based variant, for inspecting a cache file without installing Go.

Depends on `lytecache-go` v0.2.0+ (for `Cache.Inspect` and `Cache.Maintain` — see that repo's changelog).
