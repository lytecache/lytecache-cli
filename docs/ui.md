# lytecache ui

A local web admin console for lytecache database files -- think RedisInsight
or pgAdmin, not a cache server. It has no cache wire protocol and no
application ever connects to it; it reads and writes the files directly
through the same public [`lytecache-go`](https://github.com/lytecache/lytecache-go)
API the CLI itself uses. See the [README](../README.md) for the one-shot
`lytecache get/set/keys/...` commands this complements.

## Quickstart

```console
$ lytecache ui --db orders=/var/cache/lytecache/orders.db
first run: created /Users/you/.config/lytecache/ui.yaml
generated admin credentials -- username: admin  password: admin
change this with 'lytecache ui passwd', especially before ever binding beyond 127.0.0.1
lytecache ui listening on http://127.0.0.1:7070
read-only (pass --allow-delete to enable delete/flush/maintain)
```

Open `http://127.0.0.1:7070`, log in with `admin`/`admin`, and you're looking
at the fleet dashboard. That's the whole quickstart -- everything below is
about running this for real: more databases, deleting things, exposing it
beyond your own machine, and keeping it running in the background.

## The fleet dashboard

The landing page, before any single-database view: every configured
database side by side -- key count, size on disk (with a usage bar against
`max_bytes`/`max_keys` where those are known), hit rate, evictions,
expired-removed, namespace count, and the file's last-modified time.
Auto-refreshes every 2 seconds by default (pause it with the checkbox at
the top -- your preference is remembered in the browser).

A row is flagged **unhealthy** when usage crosses ~85% of a configured cap,
or when it has a non-zero **expired present** count -- that specific signal
is a sweeper-health check: it counts keys past their expiry that haven't
been swept yet, and should sit at or near zero on a healthy cache. A
growing value means the sweeper isn't keeping up and is worth investigating
on the application side, not something this UI can fix for you.

Click any row to drill into that database's key browser, where the sidebar
lists every namespace in the file **with its key count** -- a silent
namespace mismatch (looking at `default` when your app writes to `sessions`)
is the single most confusing failure mode people hit with this tool, so the
UI tries hard to make it impossible to miss.

## Which files it manages

Decided entirely by the operator at startup -- there is no "add database"
button in the browser, on purpose (see "Read-only by default" below for why
the UI's capabilities in general are deliberately narrow):

```console
$ lytecache ui --db orders=/data/orders.db --db sessions=/data/sessions.db
$ lytecache ui --scan /var/cache/lytecache        # picks up every *.db file there
```

`--db name=path` is repeatable. `--scan dir` is also repeatable, and looks
one level deep (not recursively) for `*.db` files, deriving each one's
display name from its filename. Config-file `databases:` entries (see
below), `--db` flags, and `--scan` discoveries merge in that order; a
`--db`/config-file name collision is a hard startup error (silently
overwriting a named entry would be a real surprise), while a `--scan`
collision just skips the auto-discovered duplicate with a warning.

A database that's missing, locked, or has an incompatible `schema_version`
shows an inline error on the dashboard and wherever else it's referenced,
rather than breaking the rest of the page -- and self-heals on the next
refresh if the file becomes reachable again.

## Default credentials, and when a password change is actually required

First run bootstraps `admin`/`admin` and prints it once (see the quickstart
above) -- because that default is public knowledge, two guardrails are
built in and are **not optional**:

- **Bound to `127.0.0.1` (the default):** logging in with `admin`/`admin`
  goes straight to the dashboard. No forced change, just a dismissible
  banner suggesting one. A purely local tool that only your own machine can
  reach shouldn't nag you every time you open it.
- **Bound to anything else:** the server refuses to start at all while the
  password is still the default -- see "Exposing it beyond localhost"
  below. If the password gets reset back to default (`lytecache ui
  reset-password`) while already bound non-locally, every authenticated
  route redirects to a forced change-password screen until a real one is
  set; nothing else is reachable until then.

Change it any time with:

```console
$ lytecache ui passwd
New password: 
Confirm password: 
password updated
```

For automated provisioning (containers, config management), set
`LYTECACHE_UI_PASSWORD` before the first run -- it's hashed once and the
default is never used at all. Locked yourself out? `lytecache ui
reset-password` restores `admin`/`admin` (and re-arms both guardrails
above) from local filesystem access.

## Multi-database configuration

`--db`/`--scan` are enough for one-off use; for anything you run
repeatedly, put the database list in `ui.yaml` instead (see its location
below) so you don't have to retype it:

```yaml
databases:
  - name: orders
    path: /data/orders.db
  - name: sessions
    path: /data/sessions.db
    max_keys: 100000    # optional -- see "usage bars" below
    max_bytes: 536870912
```

`max_keys`/`max_bytes` here are purely for the dashboard's usage bars and
health checks -- they're informational hints you declare, not something the
UI enforces. The library has no way to discover an application's *own*
configured capacity limits from the file alone (they're an in-memory
setting on the application's own `Cache` instance, never written to disk),
so if you want usage-against-cap bars, tell the UI what the application
was configured with.

## The delete-only capability model

This UI can remove data. It cannot create or alter it -- not from a menu
you haven't found, not from a crafted request. There is no route, gated or
otherwise, that accepts a new value or an edited one: the handler simply
doesn't exist, so there's nothing to reach. Every mutating route the UI
does have is exactly one of:

| Action | What it does |
|---|---|
| Delete a key | Removes one key, confirmation dialog first |
| Delete selected | Bulk delete, confirmation shows the count |
| Flush a namespace | Requires typing the namespace name back, not just a checkbox |
| Maintenance pass | Runs the expire sweep + eviction enforcement on demand |
| Vacuum | Reclaims disk space from already-deleted rows |

These caches back a payments system. A UI that could inject or alter a
cached record -- an OTP, an amount, an idempotency claim -- would be an
authorization bypass waiting to happen. Deletion carries no such risk: by
definition, anything worth caching is something the application can
recompute or re-fetch. That asymmetry is the entire reason this tool is
shaped the way it is, and it's enforced at the routing layer, not a
permission check that a clever request could route around.

### `--allow-delete`

Every mutation above except vacuum is hidden **and returns 403** without
this flag -- the UI is strictly read-only by default:

```console
$ lytecache ui --db orders=/data/orders.db --allow-delete
```

Vacuum is the one exception: it reclaims space from rows already deleted by
something else, so gating it behind `--allow-delete` would be protecting
against the wrong thing.

There's no config-file equivalent for this flag on purpose -- a `ui.yaml`
silently granting destructive capability if it got copied to the wrong
place would be exactly the kind of surprise this tool is trying to avoid.
It's CLI-flag-only, decided fresh every time the process starts.

## Sensitive values

Every value in the value viewer is masked behind a "Reveal value" click by
default -- nothing sensitive is on screen until you deliberately ask for
it. For values that should never be revealable at all (OTPs, tokens,
anything you'd rather not have on screen even behind a click), permanently
redact them by key pattern:

```console
$ lytecache ui --mask-keys '*otp*' --mask-keys '*:code'
```

A masked key's value is never fetched from the database at all, let alone
rendered -- stronger than the default reveal-behind-a-click treatment,
which does still put the real value in the page.

## Audit log

Every write action -- timestamp, username, remote IP, action, database,
namespace, key -- is appended to a log file next to `ui.yaml`. Values are
never logged, only key names. Nothing to configure; it's always on
whenever `--allow-delete` makes there be anything to log.

## `/metrics`

A standard Prometheus text-exposition endpoint, via
[`prometheus/client_golang`](https://pkg.go.dev/github.com/prometheus/client_golang),
computed fresh from the same read-only library calls the dashboard uses --
a scrape never writes to, or perturbs the LRU state of, any file it reads.
Results are cached for 5 seconds by default (`--metrics-cache-ttl`), so a
tight scrape interval can't hammer the underlying files.

```console
$ curl -s http://127.0.0.1:7070/metrics | grep ^lytecache_
lytecache_keys_total{database="orders",namespace="default"} 1523
lytecache_size_bytes{database="orders",namespace="default"} 48213
lytecache_expired_present_total{database="orders",namespace="default"} 0
lytecache_schema_version{database="orders"} 1
lytecache_file_readable{database="orders"} 1
lytecache_hits_total{database="orders"} 0
lytecache_misses_total{database="orders"} 0
lytecache_evictions_total{database="orders"} 0
lytecache_expired_removed_total{database="orders"} 0
lytecache_ui_scrape_duration_seconds 0.0011
```

**`hits_total`/`misses_total` will read 0 forever, and that's correct, not
broken.** They're per-process counters on *this UI's own* `Cache` handle
(see `lytecache-go`'s `Stats` doc comment) -- the UI never serves
application read traffic, so it never records a hit or a miss. They're
exposed anyway (as gauges, not counters -- see below) because
`evictions_total`/`expired_removed_total` on this same handle *do* move if
you run a maintenance pass from this UI.

All four of `hits_total`/`misses_total`/`evictions_total`/`expired_removed_total`
are Prometheus **gauges**, deliberately, not **counters**: a Counter implies
monotonic-since-process-start semantics, which would misrepresent a value
that's actually a per-process, non-cumulative snapshot.

Scrape config:

```yaml
scrape_configs:
  - job_name: lytecache-ui
    static_configs:
      - targets: ["localhost:7070"]
    # Only when bound beyond loopback -- see "Exposing it beyond
    # localhost" below.
    # authorization:
    #   type: Bearer
    #   credentials: "<your --metrics-token value>"
```

Suggested alerts:

```yaml
groups:
  - name: lytecache-ui
    rules:
      - alert: LytecacheEvictionsRising
        expr: increase(lytecache_evictions_total[1h]) > 0
        for: 10m
        annotations:
          summary: "{{ $labels.database }} is evicting keys"

      - alert: LytecacheUsageNearCap
        expr: lytecache_size_bytes / lytecache_max_bytes > 0.85
        for: 15m
        annotations:
          summary: "{{ $labels.database }} is above 85% of max_bytes"

      - alert: LytecacheExpiredPresentGrowing
        expr: increase(lytecache_expired_present_total[1h]) > 0
        for: 15m
        annotations:
          summary: "{{ $labels.database }}/{{ $labels.namespace }}'s sweeper isn't keeping up"

      - alert: LytecacheFileUnreadable
        expr: lytecache_file_readable == 0
        for: 5m
        annotations:
          summary: "{{ $labels.database }} is not readable by lytecache ui"
```

`/metrics` is exempt from the login-page requirement everywhere else in
this UI (a Prometheus scraper can't do an interactive login) but is still
protected by default -- see the guardrail in the next section. `--no-metrics`
removes the endpoint entirely if you don't want it at all.

## SSH tunnelling (the preferred way to reach a remote instance)

Before reaching for `--host 0.0.0.0`, consider whether you actually need
to bind beyond loopback at all. If `lytecache ui` is running on a remote
box, an SSH tunnel gets you the exact same `127.0.0.1:7070` experience
without ever exposing the port:

```console
$ ssh -L 7070:localhost:7070 you@remote-host
```

Then open `http://127.0.0.1:7070` locally, same as if it were running on
your own machine. No exposure guardrails to think about, no TLS
certificate to manage, nothing to lock down later -- this should be your
default for anything not running on the same machine as your browser.

## Exposing it beyond localhost

If you do need `lytecache ui` reachable from other machines directly (a
shared team instance, a container that needs its port published), the
guardrails below are mandatory and cannot be turned off by any flag:

- **The admin password must already be changed.** `--host 0.0.0.0` (or any
  non-loopback address) with the password still `admin` refuses to start
  outright -- not a warning, a hard exit. Run `lytecache ui passwd` first,
  or set `LYTECACHE_UI_PASSWORD` before the very first start.
- **Either TLS or an explicit acknowledgement.** Pass `--tls-cert`/`--tls-key`,
  or pass `--insecure` to proceed over plain HTTP anyway (a prominent
  warning is printed every time). There's no silent third option.
- **A metrics token, if `/metrics` is enabled.** Binding beyond loopback
  with metrics on and no `--metrics-token` also refuses to start -- pass a
  token, or `--no-metrics`.

```console
$ lytecache ui --host 0.0.0.0 --tls-cert cert.pem --tls-key key.pem \
    --metrics-token "$(openssl rand -hex 32)"
```

## Docker

See [`examples/docker-compose.yml`](../examples/docker-compose.yml) for
complete, working service definitions -- the highlights:

- `--host 0.0.0.0` is required *inside* the container for Docker's port
  publishing to reach it at all (a process bound to `127.0.0.1` inside its
  own network namespace is unreachable from outside it, published port or
  not) -- the exposure guardrails above apply exactly the same way inside
  a container as anywhere else.
- The `ports:` mapping can (and, for a local Compose stack, should)
  re-restrict the *host* side back to `127.0.0.1:7070:7070`, so it's only
  reachable from the machine actually running Docker.
- **Mount the config volume, not just the cache data volume.** `ui.yaml`
  (credentials, session secret) lives at
  `/home/lytecache/.config/lytecache` inside the image -- without a volume
  there too, the admin password resets to `admin`/`admin` on every
  container recreation.
- A `--scan`-based fleet view across several services' caches works from
  one shared volume with distinctly-named files (`--scan` doesn't
  recurse into per-service subdirectories) -- see the compose example's
  `lytecache-ui-fleet` service and its comments for the full explanation.

## Running as a background service

After installing (`go install`, Homebrew, Scoop, or a `.deb`/`.rpm`), `lytecache
ui` can also run as a service managed by your OS -- surviving logout and
restarting on boot, the same experience as `brew services start redis` or
`systemctl enable redis`. This is entirely additive: `lytecache ui --port
7070` as a plain foreground process, shown throughout this doc so far,
keeps working exactly the same either way.

Every flag `lytecache ui` accepts is also accepted by `lytecache service
install`, and gets persisted so the service starts identically every time.

### macOS

```console
$ lytecache service install --db orders=/data/orders.db --allow-delete
installed lytecache-ui (user LaunchAgent, ~/Library/LaunchAgents)
start it with: lytecache service start
$ lytecache service start
```

Installs a **user-level** LaunchAgent under `~/Library/LaunchAgents` -- no
`sudo` needed. If you installed via Homebrew, `brew services start
lytecache` does the same thing, since that's what most people reach for
first on macOS.

### Linux

```console
$ lytecache service install --db orders=/data/orders.db
$ lytecache service start
```

Installs a **user** systemd unit under `~/.config/systemd/user/` by
default -- no root required, and the service runs as whoever installed it
(so it can actually read the cache files that user owns). Pass `--system`
to `service install` for a system-wide unit under `/etc/systemd/system/`
instead (requires root). If systemd isn't available, kardianos/service (the
library this is built on) falls back to Upstart, OpenRC, or SysV
automatically; if none of those are present either, `service install`
reports a clear error rather than silently doing nothing.

### Windows

```console
$ lytecache service install --db orders=/data/orders.db
```

Run this from an **elevated** (Run as Administrator) shell to register
with the Service Control Manager. From a non-elevated shell, it
automatically falls back to a Scheduled Task that starts at logon instead
-- and tells you which one it used. `lytecache service status` reports
which mechanism is active for a given install.

### Everywhere

```console
$ lytecache service status
install method: os-service
status:         running
pid:            12345
uptime:         2h14m3s
bound address:  127.0.0.1:7070
config:         /Users/you/.config/lytecache/ui.yaml
log:            /Users/you/Library/Logs/lytecache/lytecache-ui.log

$ lytecache service logs --lines 50
$ lytecache service stop
$ lytecache service restart
$ lytecache service uninstall
$ lytecache ui open              # opens the running instance in your browser
```

Logs go to an OS-appropriate location (`~/Library/Logs/lytecache/` on
macOS, `$XDG_STATE_HOME/lytecache/` or `~/.local/state/lytecache/` on
Linux, `%LOCALAPPDATA%\lytecache\logs\` on Windows) with simple size-based
rotation. A startup failure (port already in use, a `ui.yaml` with loose
permissions) exits non-zero with a clear message instead of silently
flapping under the service manager's restart policy -- that's deliberate:
masking a misconfiguration behind endless auto-restarts would make it
harder to notice, not easier.

Then open `http://127.0.0.1:7070`.
