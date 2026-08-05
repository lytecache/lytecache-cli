#!/usr/bin/env bash
# Builds (unless IMAGE_TAG already points at a built image) and exercises
# the lytecache CLI's Docker image end to end: size, --version/--help,
# bind-mount and named-volume read/write across separate `docker run`
# invocations, WAL/SHM file creation with no permission errors under the
# non-root UID, and the interactive REPL. Exits non-zero on the first
# failure.
#
# Usage:
#   scripts/test-docker-image.sh                # builds lytecache:test itself
#   IMAGE_TAG=lytecache:ci scripts/test-docker-image.sh   # tests an already-built image
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

IMAGE_TAG="${IMAGE_TAG:-lytecache:test}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"; docker volume rm -f lytecache-docker-test-vol >/dev/null 2>&1 || true' EXIT

pass() { echo "  ok: $1"; }
fail() { echo "  FAIL: $1" >&2; exit 1; }

echo "== Build =="
if [ -n "${SKIP_BUILD:-}" ]; then
	echo "SKIP_BUILD set -- using existing image $IMAGE_TAG"
else
	if ! docker build -f Dockerfile.cli -t "$IMAGE_TAG" --build-arg VERSION=test .; then
		cat >&2 <<'EOF'

Build failed. If the error is a `go mod download`/`replace` failure naming
github.com/lytecache/lytecache-go, this is the known ordering dependency
documented in RELEASING.md: lytecache-go must be published to a fetchable
repo before this Dockerfile can build on its own (go.mod's `replace` is
local-development-only and isn't visible inside the Docker build context).
This is not a Dockerfile defect -- see RELEASING.md before changing anything
here.
EOF
		exit 1
	fi
fi

echo "== Image size =="
SIZE_BYTES="$(docker image inspect "$IMAGE_TAG" --format='{{.Size}}')"
SIZE_MB="$((SIZE_BYTES / 1024 / 1024))"
echo "  $IMAGE_TAG: ${SIZE_MB} MB (${SIZE_BYTES} bytes)"
if [ "$SIZE_MB" -gt 15 ]; then
	echo "  warning: image is larger than the ~15 MB scratch budget -- check for an accidental base-image change" >&2
fi

echo "== --version =="
OUT="$(docker run --rm "$IMAGE_TAG" --version)"
echo "  $OUT"
[[ "$OUT" == lytecache\ version* ]] || fail "--version did not print the expected banner"
pass "--version"

echo "== --help =="
OUT="$(docker run --rm "$IMAGE_TAG" --help)"
echo "$OUT" | grep -q "Available Commands" || fail "--help did not print command list"
pass "--help"

test_rw_cycle() {
	# Mount spec ("host-path-or-volume:/var/cache/lytecache") as its own
	# argument, not pre-joined with "-v" into one string -- that would force
	# choosing between passing it unquoted (fragile word-splitting) or quoted
	# as a single malformed flag. An array avoids both.
	local mount_spec="$1"
	local label="$2"
	local mount_args=(-v "$mount_spec")

	docker run --rm "${mount_args[@]}" "$IMAGE_TAG" set greeting '{"hi":"there"}' \
		|| fail "$label: set failed"

	OUT="$(docker run --rm "${mount_args[@]}" "$IMAGE_TAG" get greeting)"
	echo "$OUT" | grep -q '"hi": "there"' \
		|| fail "$label: get in a separate container did not see the earlier set (volume isn't persisting data)"
	pass "$label: data survives across separate docker run invocations"

	OUT="$(docker run --rm "${mount_args[@]}" "$IMAGE_TAG" keys '*')"
	[ "$OUT" = "greeting" ] || fail "$label: keys did not list the key written earlier"
	pass "$label: keys"

	OUT="$(docker run --rm "${mount_args[@]}" "$IMAGE_TAG" stats)"
	echo "$OUT" | grep -q "keys:            1" || fail "$label: stats did not report 1 key"
	pass "$label: stats"

	docker run --rm "${mount_args[@]}" "$IMAGE_TAG" flush --yes || fail "$label: flush failed"
}

echo "== Bind mount: set / get / keys / stats =="
mkdir -p "$WORKDIR/bind"
test_rw_cycle "$WORKDIR/bind:/var/cache/lytecache" "bind mount"

echo "== Named volume: set / get / keys / stats =="
docker volume create lytecache-docker-test-vol >/dev/null
test_rw_cycle "lytecache-docker-test-vol:/var/cache/lytecache" "named volume"

echo "== UID 65532 / permission check =="
LS_OUT="$(ls -la "$WORKDIR/bind")"
echo "$LS_OUT" | grep -q "cache.db" || fail "no cache.db was ever created on the bind mount"
pass "bind-mounted directory is writable by the container's non-root UID (no SQLITE_CANTOPEN surfaced above)"

echo "== WAL/SHM files appear while a connection is held open, with no permission errors =="
rm -rf "$WORKDIR/wal"
mkdir -p "$WORKDIR/wal"
(
	printf 'set during-session ok\n'
	sleep 2
	printf 'exit\n'
) | docker run --rm -i -v "$WORKDIR/wal:/var/cache/lytecache" "$IMAGE_TAG" >"$WORKDIR/repl.log" 2>&1 &
REPL_PID=$!
sleep 1
if [ ! -e "$WORKDIR/wal/cache.db-wal" ] || [ ! -e "$WORKDIR/wal/cache.db-shm" ]; then
	wait "$REPL_PID" || true
	cat "$WORKDIR/repl.log" >&2
	fail "cache.db-wal/cache.db-shm were not created while the REPL held a connection open"
fi
pass "cache.db-wal and cache.db-shm exist on the mounted directory while the connection is open"
wait "$REPL_PID"
REPL_EXIT=$?
[ "$REPL_EXIT" -eq 0 ] || { cat "$WORKDIR/repl.log" >&2; fail "REPL session exited non-zero ($REPL_EXIT)"; }
grep -qi "denied\|SQLITE_CANTOPEN\|permission" "$WORKDIR/repl.log" && fail "REPL log mentions a permission/open error"
pass "no permission errors while WAL files were in use"

echo "== Interactive REPL opens on bare invocation and exits cleanly on Ctrl-D (EOF) =="
rm -rf "$WORKDIR/repl2"
mkdir -p "$WORKDIR/repl2"
OUT="$(docker run --rm -i -v "$WORKDIR/repl2:/var/cache/lytecache" "$IMAGE_TAG" </dev/null 2>&1)"
echo "$OUT" | grep -q "Using database:" || fail "bare invocation did not open the REPL (got: $OUT)"
pass "bare \`docker run <image>\` opens the REPL and Ctrl-D (EOF) exits cleanly"

echo
echo "All Docker image checks passed."
