#!/usr/bin/env bash
# selftest.sh is an offline shell harness (deliberately NOT a *_test.go
# file — see the guard-cleanliness decision in _mill/discussion.md) that
# exercises run.sh's build/reuse/lock mechanic entirely via
# PROWLER_BUILD_ONLY=1, with no network and no Chrome, so it is
# deterministic given `go` on PATH and the nested module's deps fetchable
# or already cached.
#
# It self-locates PLUGIN_ROOT the same way run.sh does, then runs a
# numbered sequence of assertions, printing a PASS/FAIL summary and
# exiting non-zero if any assertion failed.
#
# NOT covered here (documented, not asserted — see _mill/discussion.md):
# the go-missing path and the failed-build path. Stripping `go` from PATH
# without also losing coreutils (mkdir/mv/stat/date, all needed by run.sh
# itself) is not portable enough to assert in this harness; both are
# manual checks per the batch plan's "Batch Tests" section.
set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="$SCRIPT_DIR/.."
RUN_SH="$PLUGIN_ROOT/scripts/run.sh"
BIN="$PLUGIN_ROOT/bin/prowler"
LOCK="$PLUGIN_ROOT/bin/.build.lock"

failures=0

# fail records one failed assertion and keeps going, so a single bad
# assertion doesn't hide the rest of the sequence's results.
fail() {
    echo "FAIL: $1" >&2
    failures=$((failures + 1))
}

pass() {
    echo "PASS: $1"
}

bin_present_nonempty() {
    [ -s "$BIN" ]
}

clean_bin() {
    find "$PLUGIN_ROOT/bin" -mindepth 1 -delete 2>/dev/null
    rmdir "$PLUGIN_ROOT/bin" 2>/dev/null
    true
}

bin_mtime() {
    stat -c %Y "$BIN" 2>/dev/null || stat -f %m "$BIN" 2>/dev/null
}

echo "=== prowler selftest: build-on-first-run / lock harness ==="

# --- Test 1: clean build path ---------------------------------------------
clean_bin
PROWLER_BUILD_ONLY=1 bash "$RUN_SH"
status=$?
if [ "$status" -eq 0 ] && bin_present_nonempty; then
    pass "clean build: exit 0, binary present and non-empty"
else
    fail "clean build: exit=$status, binary present=$(bin_present_nonempty && echo yes || echo no)"
fi

# --- Test 2: reuse path (no rebuild, mtime unchanged) ----------------------
before_mtime="$(bin_mtime)"
PROWLER_BUILD_ONLY=1 bash "$RUN_SH"
status=$?
after_mtime="$(bin_mtime)"
if [ "$status" -eq 0 ] && [ "$before_mtime" = "$after_mtime" ] && [ -n "$before_mtime" ]; then
    pass "reuse: exit 0, binary mtime unchanged ($before_mtime)"
else
    fail "reuse: exit=$status, mtime before=$before_mtime after=$after_mtime"
fi

# --- Test 3: concurrent builds don't corrupt the binary --------------------
clean_bin
PROWLER_BUILD_ONLY=1 bash "$RUN_SH" &
pid_a=$!
PROWLER_BUILD_ONLY=1 bash "$RUN_SH" &
pid_b=$!
wait "$pid_a"
status_a=$?
wait "$pid_b"
status_b=$?
if [ "$status_a" -eq 0 ] && [ "$status_b" -eq 0 ] && bin_present_nonempty; then
    pass "concurrent builds: both exit 0, binary present and non-empty"
else
    fail "concurrent builds: status_a=$status_a status_b=$status_b binary present=$(bin_present_nonempty && echo yes || echo no)"
fi

# --- Test 4: orphaned (stale) lock is reclaimed, not waited out ------------
clean_bin
mkdir -p "$PLUGIN_ROOT/bin"
mkdir "$LOCK"
echo "999999999-0" > "$LOCK/owner"
if touch -d '400 seconds ago' "$LOCK" 2>/dev/null; then
    start="$(date +%s)"
    PROWLER_BUILD_ONLY=1 bash "$RUN_SH"
    status=$?
    end="$(date +%s)"
    elapsed=$((end - start))
    if [ "$status" -eq 0 ] && bin_present_nonempty && [ "$elapsed" -lt 250 ]; then
        pass "stale lock reclaimed promptly (${elapsed}s), binary present"
    else
        fail "stale lock: exit=$status elapsed=${elapsed}s binary present=$(bin_present_nonempty && echo yes || echo no)"
    fi
else
    echo "SKIP: stale-lock backdating — this platform's touch does not support -d"
fi

# --- Test 5: exec-branch releases the lock before exec ---------------------
clean_bin
bash "$RUN_SH" >/dev/null 2>&1
# The wrapper builds (no PROWLER_BUILD_ONLY, no URL args), then execs the
# binary, which prints its Usage line to stderr and exits 1 — that
# non-zero exit is expected and is not itself a failure of this assertion.
if bin_present_nonempty && [ ! -d "$LOCK" ]; then
    pass "exec branch: binary present, lock released before exec"
else
    fail "exec branch: binary present=$(bin_present_nonempty && echo yes || echo no), lock dir present=$([ -d "$LOCK" ] && echo yes || echo no)"
fi

clean_bin

echo "==========================================================="
if [ "$failures" -eq 0 ]; then
    echo "PASS: all selftest assertions passed"
    exit 0
fi
echo "FAIL: $failures selftest assertion(s) failed"
exit 1
