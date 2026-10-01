#!/usr/bin/env bash
# run.sh is the build-on-first-run wrapper the prowler skill invokes. It
# self-locates the plugin root from $0 (no dependency on
# CLAUDE_PLUGIN_ROOT/CLAUDE_SKILL_DIR inside this script), builds the nested
# Go module into bin/prowler[.exe] on first use under a mkdir-atomic lock,
# and reuses that binary on every later invocation. Only the executed
# binary's stdout may reach this script's stdout — every diagnostic here and
# all `go build` output goes to stderr, so `path=$(bash run.sh ...)` always
# captures exactly one clean path line.
#
# PROWLER_BUILD_ONLY, when non-empty, selects ONLY the final action (exit 0
# instead of exec) — it never affects the reuse-vs-rebuild decision, so a
# second PROWLER_BUILD_ONLY=1 run reuses an existing binary untouched.
set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="$SCRIPT_DIR/.."

BIN="$PLUGIN_ROOT/bin/prowler"
case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) BIN="$BIN.exe" ;;
esac

LOCK="$PLUGIN_ROOT/bin/.build.lock"
MYTOKEN="$$-$RANDOM"
LOCK_TIMEOUT=300

# run_or_exit performs the wrapper's single final action. It first releases
# the build lock IF this process owns it (token-checked, so it is a safe
# no-op on the reuse path where no lock was ever acquired). It must run
# before the final action because `exec` replaces this shell's process
# image, so the EXIT trap below never fires on that branch — without this
# explicit release the lock dir would be orphaned on every real run.
run_or_exit() {
    if [ "$(cat "$LOCK/owner" 2>/dev/null)" = "$MYTOKEN" ]; then
        rm -rf "$LOCK"
    fi
    if [ -n "${PROWLER_BUILD_ONLY:-}" ]; then
        exit 0
    fi
    exec "$BIN" "$@"
}

# Reuse path: a binary already exists, independent of PROWLER_BUILD_ONLY.
# No rebuild, no lock, mtime untouched.
if [ -s "$BIN" ]; then
    run_or_exit "$@"
fi

mkdir -p "$PLUGIN_ROOT/bin"

start_time=$(date +%s)
while true; do
    if mkdir "$LOCK" 2>/dev/null; then
        # Lock acquired. Write the owner token and install the token-checked
        # exit trap in the same step: it removes the lock ONLY if this
        # process still owns it, so if a waiter wrongly reclaimed our lock
        # (staleness race), our later exit can never delete the new
        # holder's lock.
        echo "$MYTOKEN" > "$LOCK/owner"
        # shellcheck disable=SC2064
        trap "if [ \"\$(cat '$LOCK/owner' 2>/dev/null)\" = \"$MYTOKEN\" ]; then rm -rf '$LOCK'; fi" EXIT
        break
    fi

    # Another process holds the lock. If it already finished, run its
    # output instead of waiting further.
    if [ -s "$BIN" ]; then
        run_or_exit "$@"
    fi

    # Lock-age staleness: a builder that died between mkdir and
    # binary-write (e.g. SIGKILL) leaves an orphaned lock the EXIT trap
    # never ran for. Reclaim it once it is older than the deadline.
    if [ -d "$LOCK" ]; then
        lock_mtime="$(stat -c %Y "$LOCK" 2>/dev/null || stat -f %m "$LOCK" 2>/dev/null)"
        now="$(date +%s)"
        if [ -n "$lock_mtime" ] && [ $((now - lock_mtime)) -gt "$LOCK_TIMEOUT" ]; then
            rm -rf "$LOCK"
            continue
        fi
    fi

    now="$(date +%s)"
    if [ $((now - start_time)) -ge "$LOCK_TIMEOUT" ]; then
        echo "prowler: timed out waiting for the build lock ($LOCK) — if no build is actually in progress, remove it manually: rm -rf $LOCK" >&2
        exit 1
    fi
    sleep 1
done

if ! command -v go >/dev/null 2>&1; then
    echo "prowler: Go toolchain not found — install Go or add it to PATH" >&2
    exit 1
fi

TMP="$BIN.tmp.$MYTOKEN"
# Build from inside PLUGIN_ROOT (in a subshell, so the cd never leaks back
# to the rest of this script or its caller). `go build` resolves module
# context from ITS OWN working directory, not merely from the absolute
# path given as an argument — a caller invoking this wrapper from any
# other repo (prowler's whole point) would otherwise have `go build`
# resolve against that repo's own go.mod instead of the nested prowler
# module, and fail with "main module ... does not contain package ...".
if ! (cd "$PLUGIN_ROOT" && go build -o "$TMP" .) >&2; then
    echo "prowler: build failed — see the go build output above" >&2
    rm -f "$TMP"
    exit 1
fi
# Atomic on the same filesystem: no reader ever sees a partial $BIN, and
# concurrent builders' renames are last-writer-wins with a complete binary
# either way.
mv -f "$TMP" "$BIN"

run_or_exit "$@"
