# lytecache-cli

A command-line tool for inspecting and manipulating [lytecache](https://github.com/lytecache/lytecache-go) database files — think `redis-cli`, but for a file instead of a server. It's built entirely on the [lytecache-go](https://github.com/lytecache/lytecache-go) library's public API, with no cache logic duplicated here, so anything a Go program can do to a cache file, this CLI can do from a shell or a script.

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

**Manual download:** grab the archive for your OS/arch from the [latest release](https://github.com/lytecache/lytecache-cli/releases/latest); each one comes with a `checksums.txt`. Linux users can use the `.deb`/`.rpm` package from the same release instead.

## A session

```console
$ lytecache set user:42 '{"name":"Samson"}'
$ lytecache get user:42
{
  "name": "Samson"
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
  "name": "Samson"
}
lytecache (cache.db | ns: default)> exit
```

Every command works the same whether you run it one-shot, as above, or type it at the REPL prompt. The REPL is really just a loop around the same command tree, and it keeps one connection open for the whole session instead of reopening the file for every line. Command names are case-insensitive there too (`GET` and `get` both work), Ctrl-C cancels whatever you've typed so far without exiting, and Ctrl-D or `quit`/`exit` leaves.

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
| `dump <key>` | Raw row metadata: value type code/name, timestamps, sizes. The view you reach for when debugging. |
| `watch [interval]` | Redraw `stats` every `interval` seconds (default 2) until Ctrl-C. |

Global flags: `--db <path>`, `--namespace <name>` (default `default`), `--quiet` (suppress decoration), `--version`.

## Admin UI

`lytecache ui` starts a local web admin console — a RedisInsight/pgAdmin-style tool for inspecting and cleaning up cache files across several databases at once, not a cache server (no wire protocol, nothing an application ever connects to):

```console
$ lytecache ui --db orders=/data/orders.db
lytecache ui listening on http://127.0.0.1:7070
```

Fleet dashboard, a namespace-aware key browser, a value viewer, cross-database search, and delete-only mutations (`--allow-delete`) — see **[docs/ui.md](docs/ui.md)** for the full guide, including multi-database setup, `/metrics` for Prometheus, running it as a background service (`lytecache service install`), and the exposure guardrails that apply before you point it at anything beyond your own machine.

## Database resolution

Every command resolves which file to open in this order:

1. `--db <path>` flag
2. `LYTECACHE_PATH` environment variable
3. the library's own default, `lytecache.DefaultPath()` (see [lytecache-go's README](https://github.com/lytecache/lytecache-go#where-is-my-data) for how that's derived)

In interactive mode, the resolved path is printed to stderr on startup, and `lytecache which` reports it standalone in either mode. Read-only commands like `get`, `keys`, and `stats` fail with the resolved path in the error message if the file doesn't exist. Write commands like `set` and `incr` create it instead, matching how the library itself behaves.

One-shot commands open the database, do exactly one thing, and close it again. They never hold a connection open longer than necessary, which is what makes it safe to run `lytecache get`, `keys`, `stats`, or `dump` against a file a live application already has open — WAL mode handles the concurrent reads safely.

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

Values always go to stdout and diagnostics to stderr, so `lytecache get key | jq .` just works. `--quiet` suppresses banners, prompts, and confirmations, and `NO_COLOR` is respected too.

## Cross-language party trick

Every lytecache implementation shares one on-disk format (see [SPEC.md](https://github.com/lytecache/lytecache-go/blob/main/SPEC.md)), so this CLI can inspect a cache file written by a completely different language. Say a Python process wrote this:

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

No Python installation, no shared server, no export step: just the same SQLite file on disk. The two type codes this CLI can't decode — 5 for Python pickle, 6 for Java serialization — render as `(non-portable value: python-pickle, N bytes)` instead of erroring out or dumping garbage.

## Relationship to lytecache-go

This repo contains only the CLI (`cmd/lytecache`) and depends on [`github.com/lytecache/lytecache-go`](https://github.com/lytecache/lytecache-go) like any other consumer. It has no special access to that module's unexported cache logic, and doesn't duplicate any of it. See that repo for the Go library itself, the on-disk format (`SPEC.md`), and general background on lytecache.

## Docker

`lytecache` — just the CLI, packaged as a container — is published as a multi-arch image at `ghcr.io/lytecache/lytecache`. This is **not a cache server image**: `lytecache get`/`set`/`keys`/etc. run one command and exit, exactly like the native binary, because lytecache itself isn't a cache server — see the main [README](https://github.com/lytecache/lytecache-go#readme) if that's surprising. `lytecache ui` (see [below](#admin-ui) and [docs/ui.md](docs/ui.md)) is the one long-running exception: a local web admin console, not a cache endpoint.

### The primary pattern: inspecting an app's live cache

Mount the same volume your application mounts at `/var/cache/lytecache`, and set `LYTECACHE_PATH` to the same value the app uses. The CLI then needs no `--db` flag at all — it resolves `LYTECACHE_PATH` exactly the way the libraries do:

```bash
docker run --rm \
  -v myapp-cache:/var/cache/lytecache \
  -e LYTECACHE_PATH=/var/cache/lytecache/cache.db \
  ghcr.io/lytecache/lytecache:latest stats
```

### Adding it to your own `docker-compose.yml`

Add a service like this one alongside whatever service already runs your app — this is the whole thing, not an excerpt:

```yaml
services:
  # ... your existing app service, however it's defined ...

  lytecache-cli:
    image: ghcr.io/lytecache/lytecache:latest
    profiles: ["tools"]     # keeps it out of `docker compose up` — see below
    environment:
      LYTECACHE_PATH: /var/cache/lytecache/cache.db   # same value your app sets
    volumes:
      - lytecache-data:/var/cache/lytecache            # same volume your app mounts

volumes:
  lytecache-data:   # omit this if your app's compose file already declares it
```

The two things that make `--db` unnecessary are: the `lytecache-cli` service's `volumes:` entry names the **same volume** your app service mounts (whatever your app calls it — `lytecache-data` above is just this example's name), and its `LYTECACHE_PATH` is set to the **same path** your app uses. Get those two matching and every command below just works:

```bash
docker compose run --rm lytecache-cli stats
docker compose run --rm lytecache-cli keys 'session:*'
docker compose run --rm lytecache-cli          # interactive REPL
```

(See [`examples/docker-compose.yml`](examples/docker-compose.yml) for this same service in the context of a complete, runnable file, including the illustrative `app` service.)

Mount the volume on the **directory**, never on the `.db` file itself: WAL mode creates `cache.db-wal` and `cache.db-shm` beside `cache.db`, and a file-level mount would only ever see the one file it names.

WAL mode is also what makes this safe to run *while the app is live* — reading with the CLI never blocks, and never gets blocked by, the app's own reads or writes.

### One-liners

Inspect an arbitrary file on the host with `--db`:

```bash
docker run --rm -v "$PWD:/data" ghcr.io/lytecache/lytecache:latest --db /data/cache.db get some-key
```

Open the interactive REPL against a mounted volume (bare `docker run` with no trailing args opens the REPL, exactly like the native binary):

```bash
docker run --rm -it -v myapp-cache:/var/cache/lytecache ghcr.io/lytecache/lytecache:latest
```

A shell alias makes either form feel like a locally-installed binary:

```bash
alias lytecache='docker run --rm -it -v myapp-cache:/var/cache/lytecache ghcr.io/lytecache/lytecache:latest'
lytecache stats
```

### Running the admin UI

The same image also runs `lytecache ui` (see [Admin UI](#admin-ui) above and **[docs/ui.md](docs/ui.md)** for the full guide) — the one long-running exception to "runs one command and exits". `--host 0.0.0.0` is required *inside* the container for Docker's port publishing to reach it at all (a process bound to `127.0.0.1` in its own network namespace is unreachable from outside it, published port or not), and the config volume needs its own mount, separate from the cache data volume, or the admin password resets on every container recreation:

```bash
docker run --rm -p 127.0.0.1:7070:7070 \
  -v myapp-cache:/var/cache/lytecache \
  -v lytecache-ui-config:/home/lytecache/.config/lytecache \
  -e LYTECACHE_PATH=/var/cache/lytecache/cache.db \
  -e LYTECACHE_UI_PASSWORD=a-real-password \
  ghcr.io/lytecache/lytecache:latest ui --host 0.0.0.0 --insecure
```

Then open `http://127.0.0.1:7070`. The `-p 127.0.0.1:...` mapping keeps it reachable only from the machine running Docker, even though the container itself binds `0.0.0.0` (Docker's networking requires that, but it isn't the same thing as being reachable from anywhere else). `LYTECACHE_UI_PASSWORD` provisions a real password on first run — the UI refuses to bind beyond `127.0.0.1` at all while the password is still the public default, so skipping this would just fail to start. See [`examples/docker-compose.yml`](examples/docker-compose.yml) for a complete Compose service, including a `--scan`-based fleet view across several services' caches at once.

### Embedding the CLI in an application image

Rather than installing Go or downloading a release archive, copy the static binary straight out of the published image in your own `Dockerfile`:

```dockerfile
COPY --from=ghcr.io/lytecache/lytecache:latest /lytecache /usr/local/bin/lytecache
```

Since the binary is fully static (`CGO_ENABLED=0`), this works regardless of your own image's base — `alpine`, `debian`, `distroless`, anything.

### GHCR package visibility

GitHub Container Registry packages are **private by default**, including the very first one a publish workflow creates. After the first successful publish, a maintainer must open the package's settings on GitHub and change its visibility to Public — otherwise `docker pull ghcr.io/lytecache/lytecache` returns a 404 for anyone who isn't authenticated with access to the (private) package. See [RELEASING.md](RELEASING.md).

### Supported architectures and size

`linux/amd64` and `linux/arm64`. The default (`scratch`-based) image is a few MB — just the static binary plus an empty, correctly-owned cache directory; nothing else is in it. A `distroless`-based variant (`Dockerfile.cli.debug`) is also built, for anyone who specifically needs CA certificates or a resolvable non-root user entry that `scratch` doesn't provide.

### What this image will never be

No *cache* server mode: no wire protocol, nothing an application connects to over the network, no multi-host shared cache. `lytecache ui` (see below) does open an HTTP listener, but it's a local admin console you point a browser at, not a cache endpoint — nothing in this project ever accepts cache reads/writes over a socket. Sharing one `.db` file across multiple hosts over a network filesystem (NFS, Kubernetes `ReadWriteMany`) is unsupported — SQLite's locking depends on guarantees network filesystems don't reliably provide, so this can silently corrupt the file rather than just being slow. Keep the volume local to one host (or one node), the same way you already would for the library itself.

## License

Apache License 2.0. See [LICENSE](LICENSE).
