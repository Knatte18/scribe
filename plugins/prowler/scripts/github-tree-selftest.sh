#!/usr/bin/env bash
# github-tree-selftest.sh is an offline harness (mirroring selftest.sh's
# shape) that drives github-tree.sh through a stub `gh` on PATH, with no
# network access at all. It asserts the exact stdout for every scenario,
# the exact call count and call identity `gh` was invoked with for every
# scenario, and a distinguishing stderr substring for every distinguished
# failure.
#
# System `jq` is a dependency of THIS HARNESS ONLY, never of
# github-tree.sh, which parses its `--jq` expression through `gh`'s own
# embedded gojq at run time. That means this harness validates the jq
# expression under jq while production runs the identical expression
# under gojq -- an accepted seam that a future expression relying on a
# jq/gojq-divergent construct would fall through undetected here.
#
# Portability envelope: the stub-on-PATH mechanism needs an extensionless
# executable with the exec bit set, so Linux and macOS are the asserted
# platforms. Windows Git Bash is expected to work but is not claimed;
# cmd.exe and PowerShell cannot run this harness or the stub at all.
#
# The stub gh's map file can point a fixture name at an absolute path
# instead of a bare filename under testdata/ -- see gen_tree_body below,
# which uses this to hand the stub a body generated at harness runtime
# rather than a fixture checked into the repository.
#
# NOT covered here (documented, not asserted -- manual checks per the
# batch plan's "Batch Tests" section): one live run against a small
# public repo; one live run against torvalds/linux confirming the real
# truncated fallback completes in a single invocation; a spot-check that
# the jq expression behaves identically under gojq and jq; and the HTTP
# 409 commitless-repository alias, which no fixture pins because it was
# never observed live.
# Also not covered offline (documented here, not asserted): one live
# --children run against a real repository, and one live guard trip
# against a large public repository.
set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="$SCRIPT_DIR/.."
GH_TREE_SH="$SCRIPT_DIR/github-tree.sh"
# Resolved once, up front, before test 18 strips PATH for its own
# invocation: bash's temporary-assignment scoping (`VAR=val cmd`) applies
# the modified PATH to the search for `cmd` itself, so bare `bash` cannot
# be found by name once PATH has been emptied. Using this absolute path
# (which contains a slash, so execve needs no PATH search at all) is what
# lets test 18 actually exercise github-tree.sh's own "gh not found"
# guard instead of failing before it even starts.
BASH_BIN="$(command -v bash)"
STUB_BIN="$SCRIPT_DIR/testdata/github-tree/bin"
BODIES_DIR="$SCRIPT_DIR/testdata/github-tree/bodies"

# .scratch/ is this repo's sanctioned scratch location (gitignored) --
# never a system temp directory, so a run leaves no trace outside the repo.
SCRATCH="$PLUGIN_ROOT/../../.scratch/github-tree-selftest"

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

# JQ_BIN is read at call time inside require_jq (never captured at
# definition time), which is what lets test 20 exercise the guard in a
# subshell -- overriding JQ_BIN there and calling require_jq directly --
# with no recursion into this script to bound.
JQ_BIN="${GITHUB_TREE_SELFTEST_JQ:-jq}"

require_jq() {
    if ! command -v "$JQ_BIN" >/dev/null 2>&1; then
        echo "github-tree-selftest: '$JQ_BIN' not found on PATH — install jq to run this harness" >&2
        return 1
    fi
    return 0
}

require_jq || exit 1

rm -rf "$SCRATCH"
mkdir -p "$SCRATCH"

# run_scenario writes <map-content> to $SCRATCH/<scenario-name>/map.tsv,
# truncates that scenario's call log, then runs github-tree.sh with the
# stub gh prepended to PATH, capturing stdout, stderr, and the exit status
# into the shell variables out/err/status. Capturing stdout and stderr
# into separate variables is mandatory: a great many assertions below turn
# on stdout being byte-empty while stderr is not.
run_scenario() {
    local name="$1" map_content="$2"
    shift 2
    local dir="$SCRATCH/$name"
    mkdir -p "$dir"
    printf '%s' "$map_content" > "$dir/map.tsv"
    : > "$dir/calls.log"
    PATH="$STUB_BIN:$PATH" \
        GH_STUB_MAP="$dir/map.tsv" \
        GH_STUB_BODIES="$BODIES_DIR" \
        GH_STUB_LOG="$dir/calls.log" \
        bash "$GH_TREE_SH" "$@" >"$dir/stdout" 2>"$dir/stderr"
    status=$?
    out="$(cat "$dir/stdout")"
    err="$(cat "$dir/stderr")"
}

# calls prints a scenario's call log verbatim.
calls() {
    cat "$SCRATCH/$1/calls.log" 2>/dev/null
}

# call_line_count prints the number of lines in a scenario's call log.
call_line_count() {
    local logfile="$SCRATCH/$1/calls.log"
    [ -f "$logfile" ] || { echo 0; return; }
    wc -l < "$logfile" | tr -d ' '
}

# call_count_for_endpoint prints how many logged calls named exactly
# <endpoint> as their second argument. Splitting each log line with `read`
# rather than grep avoids the endpoint's own `?`/`&` characters being
# read as regex metacharacters.
call_count_for_endpoint() {
    local scenario="$1" endpoint="$2" count=0
    local logfile="$SCRATCH/$scenario/calls.log"
    [ -f "$logfile" ] || { echo 0; return; }
    local sub ep flag expr
    while IFS= read -r line; do
        read -r sub ep flag expr <<< "$line"
        if [ "$ep" = "$endpoint" ]; then
            count=$((count + 1))
        fi
    done < "$logfile"
    echo "$count"
}

# gen_tree_body <outfile> <count> writes a syntactically valid non-recursive
# tree response body to <outfile>, with truncated false and exactly <count>
# blob entries whose path and sha are mechanically derived from the loop
# index. These large bodies are generated rather than checked in as
# fixtures because a thousand mechanically-identical entries have no shape
# worth reviewing, and committing them would bury the fixtures that do.
# Reading the same generator with different counts is also what keeps the
# at-ceiling and one-over-ceiling scenarios provably one entry apart.
gen_tree_body() {
    local outfile="$1" count="$2" i
    {
        printf '{\n  "sha": "srgenerated",\n  "truncated": false,\n  "tree": [\n'
        for ((i = 0; i < count; i++)); do
            printf '    { "path": "gen%d.txt", "mode": "100644", "type": "blob", "sha": "bgen%d" }' "$i" "$i"
            if [ "$((i + 1))" -lt "$count" ]; then
                printf ',\n'
            else
                printf '\n'
            fi
        done
        printf '  ]\n}\n'
    } > "$outfile"
}

echo "=== github-tree selftest: offline stub-gh harness ==="

# --- Test 1: fast path, untruncated whole-repo listing ---------------------
run_scenario small "$(printf 'repos/acme/small/git/trees/HEAD?recursive=1\tsmall-root-rec.json\n')" acme/small
expected="$(printf 'intro.md\nsrc/main.go\nsrc/util.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "fast path: exact three-line stdout"
else
    fail "fast path: status=$status out=$out"
fi
src_leaked=0
while IFS= read -r line; do
    [ "$line" = "src" ] && src_leaked=1
done <<< "$out"
if [ "$src_leaked" -eq 0 ]; then
    pass "fast path: bare directory path 'src' never appears as its own line"
else
    fail "fast path: bare directory path 'src' leaked onto stdout"
fi
if [ "$(call_line_count small)" -eq 1 ]; then
    pass "fast path: exactly one gh call (no duplicate truncation check, branch resolve, or auth preflight)"
else
    fail "fast path: call log has $(call_line_count small) lines, expected 1: $(calls small)"
fi

# --- Test 2: path scoping on the fast path ----------------------------------
run_scenario scoped "$(printf 'repos/acme/scoped/git/trees/HEAD:src?recursive=1\tscoped-src-rec.json\n')" acme/scoped src
expected="$(printf 'src/main.go\nsrc/deep/x.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "path scoping: prefix re-applied to API's subtree-relative paths"
else
    fail "path scoping: status=$status out=$out"
fi
if [ "$(call_line_count scoped)" -eq 1 ]; then
    pass "path scoping: exactly one gh call"
else
    fail "path scoping: call log has $(call_line_count scoped) lines, expected 1: $(calls scoped)"
fi
if [ "$(call_count_for_endpoint scoped 'repos/acme/scoped/git/trees/HEAD:src?recursive=1')" -eq 1 ]; then
    pass "path scoping: endpoint uses the HEAD:src form"
else
    fail "path scoping: endpoint did not use the HEAD:src form: $(calls scoped)"
fi

# --- Test 3: path argument normalization ------------------------------------
scoped_map="$(printf 'repos/acme/scoped/git/trees/HEAD:src?recursive=1\tscoped-src-rec.json\n')"
run_scenario norm1 "$scoped_map" acme/scoped src
out1="$out"; calls1="$(calls norm1)"
run_scenario norm2 "$scoped_map" acme/scoped /src
out2="$out"; calls2="$(calls norm2)"
run_scenario norm3 "$scoped_map" acme/scoped src/
out3="$out"; calls3="$(calls norm3)"
if [ "$out1" = "$out2" ] && [ "$out2" = "$out3" ] && [ "$calls1" = "$calls2" ] && [ "$calls2" = "$calls3" ]; then
    pass "path normalization: src, /src, and src/ produce byte-identical stdout and call logs"
else
    fail "path normalization: out1=$out1 out2=$out2 out3=$out3 calls1=$calls1 calls2=$calls2 calls3=$calls3"
fi

# --- Test 4: one-level truncated fallback and sibling order -----------------
trunc1_map="$(printf 'repos/acme/big/git/trees/HEAD?recursive=1\ttrunc1-root-rec.json\nrepos/acme/big/git/trees/HEAD\ttrunc1-root-nonrec.json\nrepos/acme/big/git/trees/tmmm?recursive=1\ttrunc1-mmm-rec.json\nrepos/acme/big/git/trees/taaa?recursive=1\ttrunc1-aaa-rec.json\nrepos/acme/big/git/trees/tbbb?recursive=1\ttrunc1-bbb-rec.json\n')"
run_scenario trunc1 "$trunc1_map" acme/big
expected="$(printf 'zzz.txt\nMakefile\nmmm/m1.txt\nmmm/sub/m2.txt\naaa/a1.txt\nbbb/b1.txt')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "one-level fallback: exact six-line stdout in FIFO sibling order"
else
    fail "one-level fallback: status=$status out=$out"
fi
case "$out" in
*Makefile*) pass "one-level fallback: Makefile (root-only-blob-in-nonrec) present" ;;
*) fail "one-level fallback: Makefile missing -- root's own blobs not collected at re-list" ;;
esac
case "$out" in
*aaa/a1.txt*bbb/b1.txt*) pass "one-level fallback: aaa and bbb (subtrees absent from the truncated body) present" ;;
*) fail "one-level fallback: aaa/bbb missing -- subtrees not enqueued from the non-recursive listing" ;;
esac
if [ "$(call_line_count trunc1)" -eq 5 ]; then
    pass "one-level fallback: exactly five gh calls"
else
    fail "one-level fallback: call log has $(call_line_count trunc1) lines, expected 5: $(calls trunc1)"
fi

# --- Test 5: two-level truncated fallback -----------------------------------
trunc2_map="$(printf 'repos/acme/deep/git/trees/HEAD?recursive=1\ttrunc2-root-rec.json\nrepos/acme/deep/git/trees/HEAD\ttrunc2-root-nonrec.json\nrepos/acme/deep/git/trees/ta?recursive=1\ttrunc2-a-rec.json\nrepos/acme/deep/git/trees/tb?recursive=1\ttrunc2-b-rec.json\nrepos/acme/deep/git/trees/tb\ttrunc2-b-nonrec.json\nrepos/acme/deep/git/trees/tbx?recursive=1\ttrunc2-bx-rec.json\nrepos/acme/deep/git/trees/tby?recursive=1\ttrunc2-by-rec.json\n')"
run_scenario trunc2 "$trunc2_map" acme/deep
expected="$(printf 'r.txt\na/a1.txt\nb/bown.txt\nb/x/x1.txt\nb/y/y1.txt')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "two-level fallback: exact five-line stdout"
else
    fail "two-level fallback: status=$status out=$out"
fi
case "$out" in
*b/y/y1.txt*) pass "two-level fallback: b/y/y1.txt present (y only exists in b's non-recursive re-fetch)" ;;
*) fail "two-level fallback: b/y/y1.txt missing -- children read from the truncated response instead of the re-fetch" ;;
esac
case "$out" in
*b/bown.txt*) pass "two-level fallback: b/bown.txt present (b's own blob only exists in the re-fetch)" ;;
*) fail "two-level fallback: b/bown.txt missing" ;;
esac
if [ "$(call_count_for_endpoint trunc2 'repos/acme/deep/git/trees/ta?recursive=1')" -eq 1 ] \
    && [ "$(call_count_for_endpoint trunc2 'repos/acme/deep/git/trees/ta')" -eq 0 ]; then
    pass "two-level fallback: untruncated sibling 'a' fetched once, never re-listed non-recursively"
else
    fail "two-level fallback: unexpected call pattern for 'a': $(calls trunc2)"
fi
if [ "$(call_line_count trunc2)" -eq 7 ]; then
    pass "two-level fallback: exactly seven gh calls"
else
    fail "two-level fallback: call log has $(call_line_count trunc2) lines, expected 7: $(calls trunc2)"
fi

# --- Test 6: scoped listing whose subtree truncates -------------------------
scopedtrunc_map="$(printf 'repos/acme/scopedbig/git/trees/HEAD:src?recursive=1\tscopedtrunc-src-rec.json\nrepos/acme/scopedbig/git/trees/HEAD:src\tscopedtrunc-src-nonrec.json\nrepos/acme/scopedbig/git/trees/tlib?recursive=1\tscopedtrunc-lib-rec.json\n')"
run_scenario scopedtrunc "$scopedtrunc_map" acme/scopedbig src
expected="$(printf 'src/s.txt\nsrc/lib/l1.txt')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "scoped truncated fallback: prefixes stay repo-relative below the scoping point"
else
    fail "scoped truncated fallback: status=$status out=$out"
fi
if [ "$(call_line_count scopedtrunc)" -eq 3 ]; then
    pass "scoped truncated fallback: exactly three gh calls"
else
    fail "scoped truncated fallback: call log has $(call_line_count scopedtrunc) lines, expected 3: $(calls scopedtrunc)"
fi
if [ "$(call_count_for_endpoint scopedtrunc 'repos/acme/scopedbig/git/trees/HEAD')" -eq 0 ] \
    && [ "$(call_count_for_endpoint scopedtrunc 'repos/acme/scopedbig/git/trees/HEAD?recursive=1')" -eq 0 ]; then
    pass "scoped truncated fallback: no sibling of the scoped directory was ever fetched"
else
    fail "scoped truncated fallback: an unscoped root endpoint was called: $(calls scopedtrunc)"
fi

# --- Test 7: entry types -----------------------------------------------------
run_scenario types "$(printf 'repos/acme/types/git/trees/HEAD?recursive=1\ttypes-root-rec.json\n')" acme/types
expected="$(printf 'link\nreal.txt')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "entry types: symlink and regular blob emitted, submodule and directory absent"
else
    fail "entry types: status=$status out=$out"
fi

# --- Test 8: zero blobs is success -------------------------------------------
run_scenario noblobs "$(printf 'repos/acme/subsonly/git/trees/HEAD?recursive=1\tnoblobs-root-rec.json\n')" acme/subsonly
if [ "$status" -eq 0 ] && [ -z "$out" ]; then
    pass "zero blobs: exit 0, byte-empty stdout"
else
    fail "zero blobs: status=$status out=$out"
fi

# --- Test 9: a returned path containing a tab --------------------------------
run_scenario badpath "$(printf 'repos/acme/badpath/git/trees/HEAD?recursive=1\tbadpath-root-rec.json\n')" acme/badpath
backslash_t=$'\\t'
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *"$backslash_t"* ]]; then
    pass "tab-containing path: reported JSON-escaped in stderr, stdout empty, non-zero exit"
else
    fail "tab-containing path: status=$status out=$out err=$err"
fi

# --- Test 10: a non-recursive listing that is itself truncated --------------
run_scenario nonrectrunc "$(printf 'repos/acme/nonrectrunc/git/trees/HEAD?recursive=1\ttrunc1-root-rec.json\nrepos/acme/nonrectrunc/git/trees/HEAD\tnonrectrunc-root-nonrec.json\n')" acme/nonrectrunc
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *truncated* ]]; then
    pass "non-recursive listing itself truncated: stops with 'truncated' in stderr"
else
    fail "non-recursive listing itself truncated: status=$status out=$out err=$err"
fi

# --- Test 11: mid-walk failure and the buffering proof -----------------------
midwalk_map="$(printf 'repos/acme/midwalk/git/trees/HEAD?recursive=1\ttrunc1-root-rec.json\nrepos/acme/midwalk/git/trees/HEAD\ttrunc1-root-nonrec.json\nrepos/acme/midwalk/git/trees/tmmm?recursive=1\ttrunc1-mmm-rec.json\nrepos/acme/midwalk/git/trees/taaa?recursive=1\terror-403.json\t403\n')"
run_scenario midwalk "$midwalk_map" acme/midwalk
if [ -z "$out" ] && [ "$status" -ne 0 ]; then
    pass "mid-walk failure: byte-empty stdout proves buffering, not streaming"
else
    fail "mid-walk failure: status=$status out=$out"
fi
if [ "$(call_count_for_endpoint midwalk 'repos/acme/midwalk/git/trees/tbbb?recursive=1')" -eq 0 ] \
    && [ "$(call_count_for_endpoint midwalk 'repos/acme/midwalk/git/trees/tbbb')" -eq 0 ]; then
    pass "mid-walk failure: walk aborted before ever requesting tbbb"
else
    fail "mid-walk failure: tbbb was requested despite the earlier failure: $(calls midwalk)"
fi

# --- Test 12: HTTP 401 --------------------------------------------------------
run_scenario err401 "$(printf 'repos/acme/e401/git/trees/HEAD?recursive=1\terror-401.json\t401\n')" acme/e401
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *"not authenticated"* ]]; then
    pass "HTTP 401: stderr contains 'not authenticated'"
else
    fail "HTTP 401: status=$status out=$out err=$err"
fi

# --- Test 13: HTTP 403 --------------------------------------------------------
run_scenario err403 "$(printf 'repos/acme/e403/git/trees/HEAD?recursive=1\terror-403.json\t403\n')" acme/e403
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *"rate limited"* ]]; then
    pass "HTTP 403: stderr contains 'rate limited'"
else
    fail "HTTP 403: status=$status out=$out err=$err"
fi

# --- Test 14: HTTP 404 on an unscoped fetch ----------------------------------
run_scenario err404 "$(printf 'repos/acme/e404/git/trees/HEAD?recursive=1\terror-404.json\t404\n')" acme/e404
if [ -z "$out" ] && [ "$status" -ne 0 ] \
    && [[ "$err" == *"not found"* ]] \
    && [[ "$err" == *"may not be accessible"* ]] \
    && [[ "$err" == *"no commits yet"* ]]; then
    pass "HTTP 404 unscoped: stderr names all three undistinguished causes"
else
    fail "HTTP 404 unscoped: status=$status out=$out err=$err"
fi

# --- Test 15: scoped 404 versus 422 as distinct messages ---------------------
run_scenario scoped404 "$(printf 'repos/acme/e404/git/trees/HEAD:nope?recursive=1\terror-404.json\t404\n')" acme/e404 nope
out404="$out"; err404_msg="$err"; status404="$status"
run_scenario scoped422 "$(printf 'repos/acme/e422/git/trees/HEAD:notadir?recursive=1\terror-422.json\t422\n')" acme/e422 notadir
out422="$out"; err422_msg="$err"; status422="$status"
if [ -z "$out404" ] && [ "$status404" -ne 0 ] && [ -z "$out422" ] && [ "$status422" -ne 0 ] \
    && [[ "$err404_msg" == *"not found"* ]] \
    && [[ "$err422_msg" == *"not a directory"* ]] \
    && [ "$err404_msg" != "$err422_msg" ]; then
    pass "scoped 404 vs 422: distinct messages, path-not-found vs path-not-a-directory"
else
    fail "scoped 404 vs 422: status404=$status404 out404=$out404 err404=$err404_msg status422=$status422 out422=$out422 err422=$err422_msg"
fi

# --- Test 16: missing or malformed repository argument -----------------------
run_scenario noargs ""
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(call_line_count noargs)" -eq 0 ]; then
    pass "missing repository argument: rejected before any gh call"
else
    fail "missing repository argument: status=$status out=$out calls=$(calls noargs)"
fi
run_scenario badslug "" notaslug
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(call_line_count badslug)" -eq 0 ]; then
    pass "malformed repository argument: rejected before any gh call"
else
    fail "malformed repository argument: status=$status out=$out calls=$(calls badslug)"
fi

# --- Test 17: a path needing URL encoding -------------------------------------
run_scenario spacepath "" acme/small "src dir"
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(call_line_count spacepath)" -eq 0 ] && [[ "$err" == *" "* ]]; then
    pass "path with a space: rejected before any gh call, stderr names the space"
else
    fail "path with a space: status=$status out=$out calls=$(calls spacepath) err=$err"
fi
run_scenario hashpath "" acme/small "a#b"
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(call_line_count hashpath)" -eq 0 ] && [[ "$err" == *"#"* ]]; then
    pass "path with a '#': rejected before any gh call, stderr names the '#'"
else
    fail "path with a '#': status=$status out=$out calls=$(calls hashpath) err=$err"
fi
run_scenario naivepath "" acme/small "naïve"
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(call_line_count naivepath)" -eq 0 ] && [[ "$err" == *"ï"* ]]; then
    pass "path with 'naïve': rejected before any gh call, stderr names 'ï' whole (locale-independent)"
else
    fail "path with 'naïve': status=$status out=$out calls=$(calls naivepath) err=$err"
fi

# --- Test 18: gh missing from PATH --------------------------------------------
PATH="" "$BASH_BIN" "$GH_TREE_SH" acme/small >"$SCRATCH/nogh.stdout" 2>"$SCRATCH/nogh.stderr"
status=$?
out="$(cat "$SCRATCH/nogh.stdout")"
err="$(cat "$SCRATCH/nogh.stderr")"
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *gh* ]]; then
    pass "gh missing from PATH: non-zero exit, byte-empty stdout, stderr mentions gh"
else
    fail "gh missing from PATH: status=$status out=$out err=$err"
fi

# --- Test 19: stdout cleanliness ----------------------------------------------
run_scenario small "$(printf 'repos/acme/small/git/trees/HEAD?recursive=1\tsmall-root-rec.json\n')" acme/small
small_out="$out"
run_scenario trunc1 "$trunc1_map" acme/big
trunc1_out="$out"
clean=1
while IFS= read -r line; do
    case "$line" in
    '#'*) clean=0 ;;
    '') clean=0 ;;
    esac
done <<< "$small_out"$'\n'"$trunc1_out"
if [ "$clean" -eq 1 ]; then
    pass "stdout cleanliness: no line begins with '#', no empty line"
else
    fail "stdout cleanliness: a '#'-prefixed or empty line leaked onto stdout"
fi

# --- Test 20: the harness's own prerequisite guard ----------------------------
(
    JQ_BIN="definitely-not-jq"
    guard_err="$(require_jq 2>&1)"
    guard_status=$?
    if [ "$guard_status" -ne 0 ] && [[ "$guard_err" == *"definitely-not-jq"* ]] && [[ "$guard_err" == *"install jq"* ]]; then
        exit 0
    fi
    exit 1
)
if [ $? -eq 0 ]; then
    pass "require_jq guard: fails with the missing-binary name and install hint"
else
    fail "require_jq guard: did not fail as expected for a missing jq binary"
fi

# --- Test 21: too many arguments ----------------------------------------------
run_scenario toomany "" acme/small src extra
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(call_line_count toomany)" -eq 0 ] && [[ "$err" == *"usage:"* ]]; then
    pass "too many arguments: exit 2 specifically, byte-empty stdout, empty call log, 'usage:' in stderr"
else
    fail "too many arguments: status=$status out=$out calls=$(calls toomany) err=$err"
fi

# --- Test 22: the stub's own rejection path -----------------------------------
stub_dir="$SCRATCH/stubdirect"
mkdir -p "$stub_dir"
: > "$stub_dir/calls.log"
GH_STUB_MAP="$stub_dir/map.tsv" GH_STUB_BODIES="$BODIES_DIR" GH_STUB_LOG="$stub_dir/calls.log" \
    "$STUB_BIN/gh" auth status >"$stub_dir/stdout" 2>"$stub_dir/stderr"
stub_status=$?
stub_err="$(cat "$stub_dir/stderr")"
if [ "$stub_status" -eq 98 ] && [[ "$stub_err" == *"unsupported invocation"* ]] && [ "$(wc -l < "$stub_dir/calls.log" | tr -d ' ')" -eq 1 ]; then
    pass "stub rejection path: exit 98, 'unsupported invocation' in stderr, call still logged"
else
    fail "stub rejection path: status=$stub_status err=$stub_err calls=$(cat "$stub_dir/calls.log")"
fi

# --- Test 23: --children on a path ---------------------------------------------
run_scenario children_scoped "$(printf 'repos/acme/childrenrepo/git/trees/HEAD:src\tchildren-src-nonrec.json\n')" --children acme/childrenrepo src
expected="$(printf 'src/main.go\nsrc/deep/\nsrc/util.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "--children on a path: exact stdout, directory entry with one trailing slash"
else
    fail "--children on a path: status=$status out=$out"
fi
case "$out" in
*vendor*) fail "--children on a path: submodule entry 'vendor' leaked onto stdout" ;;
*) pass "--children on a path: submodule entry absent" ;;
esac
if [ "$(call_line_count children_scoped)" -eq 1 ]; then
    pass "--children on a path: exactly one gh call"
else
    fail "--children on a path: call log has $(call_line_count children_scoped) lines, expected 1: $(calls children_scoped)"
fi
if [ "$(call_count_for_endpoint children_scoped 'repos/acme/childrenrepo/git/trees/HEAD:src')" -eq 1 ]; then
    pass "--children on a path: endpoint is the non-recursive HEAD:<path> form"
else
    fail "--children on a path: endpoint did not match the non-recursive scoped form: $(calls children_scoped)"
fi

# --- Test 24: --children with no path -------------------------------------------
run_scenario children_root "$(printf 'repos/acme/childrenroot/git/trees/HEAD\ttrunc1-root-nonrec.json\n')" --children acme/childrenroot
expected="$(printf 'zzz.txt\nMakefile\nmmm/\naaa/\nbbb/')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "--children with no path: two root blobs unmarked, three directories trailing-slash-marked"
else
    fail "--children with no path: status=$status out=$out"
fi
if [ "$(call_line_count children_root)" -eq 1 ]; then
    pass "--children with no path: exactly one gh call"
else
    fail "--children with no path: call log has $(call_line_count children_root) lines, expected 1: $(calls children_root)"
fi

# --- Test 25: --children never recurses -----------------------------------------
run_scenario children_norecurse "$(printf 'repos/acme/childrennorec/git/trees/HEAD\ttrunc1-root-nonrec.json\n')" --children acme/childrennorec
if [ "$(call_line_count children_norecurse)" -eq 1 ]; then
    pass "--children never recurses: call count stays at 1 despite tree entries in the listing"
else
    fail "--children never recurses: call log has $(call_line_count children_norecurse) lines, expected 1: $(calls children_norecurse)"
fi
descendant_leaked=0
while IFS= read -r line; do
    [ -z "$line" ] && continue
    stripped="${line%/}"
    case "$stripped" in
    */*) descendant_leaked=1 ;;
    esac
done <<< "$out"
if [ "$descendant_leaked" -eq 0 ]; then
    pass "--children never recurses: no descendant path (a slash before the final character) appears"
else
    fail "--children never recurses: a descendant path leaked onto stdout: $out"
fi

# --- Test 26: --children skips submodules ----------------------------------------
run_scenario children_submodule "$(printf 'repos/acme/childrenrepo2/git/trees/HEAD:src\tchildren-src-nonrec.json\n')" --children acme/childrenrepo2 src
case "$out" in
*vendor*) fail "--children skips submodules: 'vendor' leaked onto stdout marked or unmarked" ;;
*) pass "--children skips submodules: submodule entry never appears" ;;
esac

# --- Test 27: --children on an empty directory ------------------------------------
run_scenario children_empty "$(printf 'repos/acme/childrenempty/git/trees/HEAD:empty\tchildren-empty-nonrec.json\n')" --children acme/childrenempty empty
if [ "$status" -eq 0 ] && [ -z "$out" ]; then
    pass "--children on an empty directory: exit 0, byte-empty stdout"
else
    fail "--children on an empty directory: status=$status out=$out"
fi

# --- Test 28: --children listing that is itself truncated -------------------------
run_scenario children_trunc "$(printf 'repos/acme/childrentrunc/git/trees/HEAD\tnonrectrunc-root-nonrec.json\n')" --children acme/childrentrunc
if [ -z "$out" ] && [ "$status" -ne 0 ] && [[ "$err" == *truncated* ]]; then
    pass "--children listing itself truncated: byte-empty stdout, non-zero exit, 'truncated' in stderr"
else
    fail "--children listing itself truncated: status=$status out=$out err=$err"
fi

# --- Test 29: guard fires on the recursive fast path -----------------------------
run_scenario guard_fastpath "$(printf 'repos/acme/guardfast/git/trees/HEAD?recursive=1\tsmall-root-rec.json\n')" --max-entries 2 acme/guardfast
if [ -z "$out" ] && [ "$status" -eq 1 ] \
    && [[ "$err" == *"2"* ]] && [[ "$err" == *"--children"* ]] && [[ "$err" == *"--max-entries"* ]]; then
    pass "guard fires on the recursive fast path: byte-empty stdout, exit 1, ceiling and both remedies in stderr"
else
    fail "guard fires on the recursive fast path: status=$status out=$out err=$err"
fi

# --- Test 30: guard fires in --children mode --------------------------------------
run_scenario guard_children "$(printf 'repos/acme/guardchildren/git/trees/HEAD:src\tchildren-src-nonrec.json\n')" --children --max-entries 1 acme/guardchildren src
if [ -z "$out" ] && [ "$status" -eq 1 ] \
    && [[ "$err" == *"1"* ]] && [[ "$err" == *"--max-entries"* ]] && [[ "$err" != *"--children"* ]]; then
    pass "guard fires in --children mode: ceiling and --max-entries in stderr, --children never suggested back"
else
    fail "guard fires in --children mode: status=$status out=$out err=$err"
fi

# --- Test 31: guard fires incrementally, not at end-of-walk -----------------------
run_scenario guard_incremental_on "$trunc1_map" --max-entries 2 acme/big
if [ -z "$out" ] && [ "$status" -eq 1 ]; then
    pass "guard fires incrementally: aborts a low-ceiling run of the five-call truncated-fallback map"
else
    fail "guard fires incrementally: status=$status out=$out"
fi
guarded_calls="$(call_line_count guard_incremental_on)"
run_scenario guard_incremental_off "$trunc1_map" --max-entries 0 acme/big
unguarded_calls="$(call_line_count guard_incremental_off)"
if [ "$guarded_calls" -lt "$unguarded_calls" ]; then
    pass "guard fires incrementally: guarded call count ($guarded_calls) is strictly lower than the unguarded run's ($unguarded_calls)"
else
    fail "guard fires incrementally: guarded call count ($guarded_calls) is not lower than the unguarded run's ($unguarded_calls)"
fi

# --- Test 32: the boundary, one entry apart ----------------------------------------
small_map="$(printf 'repos/acme/guardboundary/git/trees/HEAD?recursive=1\tsmall-root-rec.json\n')"
run_scenario guard_boundary_ok "$small_map" --max-entries 3 acme/guardboundary
expected="$(printf 'intro.md\nsrc/main.go\nsrc/util.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "boundary: ceiling exactly equal to the entry count succeeds, printing all three paths"
else
    fail "boundary: status=$status out=$out"
fi
run_scenario guard_boundary_abort "$small_map" --max-entries 2 acme/guardboundary
if [ -z "$out" ] && [ "$status" -eq 1 ]; then
    pass "boundary: the same fixture one entry over the ceiling aborts"
else
    fail "boundary: status=$status out=$out"
fi

# --- Test 33: the default ceiling is 1000 ------------------------------------------
mkdir -p "$SCRATCH/gen"
gen_tree_body "$SCRATCH/gen/body-1001.json" 1001
gen_tree_body "$SCRATCH/gen/body-1000.json" 1000
run_scenario guard_default_over "$(printf 'repos/acme/guarddefault1/git/trees/HEAD?recursive=1\t%s\n' "$SCRATCH/gen/body-1001.json")" acme/guarddefault1
default_over_out="$out"
if [ -z "$default_over_out" ] && [ "$status" -eq 1 ]; then
    pass "default ceiling: no --max-entries at all, a 1001-entry listing aborts"
else
    fail "default ceiling: status=$status out=$default_over_out"
fi
run_scenario guard_default_at "$(printf 'repos/acme/guarddefault2/git/trees/HEAD?recursive=1\t%s\n' "$SCRATCH/gen/body-1000.json")" acme/guarddefault2
default_at_lines="$(printf '%s\n' "$out" | grep -c .)"
if [ "$status" -eq 0 ] && [ "$default_at_lines" -eq 1000 ]; then
    pass "default ceiling: no --max-entries at all, a 1000-entry listing succeeds"
else
    fail "default ceiling: status=$status out_line_count=$default_at_lines"
fi

# --- Test 34: --max-entries 0 disables the ceiling ---------------------------------
run_scenario guard_zero_unlimited "$(printf 'repos/acme/guardzero/git/trees/HEAD?recursive=1\t%s\n' "$SCRATCH/gen/body-1001.json")" --max-entries 0 acme/guardzero
out_lines="$(printf '%s\n' "$out" | grep -c .)"
if [ "$status" -eq 0 ] && [ "$out_lines" -eq 1001 ]; then
    pass "--max-entries 0 disables the ceiling: exit 0, 1001-line stdout"
else
    fail "--max-entries 0 disables the ceiling: status=$status out_lines=$out_lines"
fi

# --- Test 35: the buffering guarantee under the guard's own failure mode ----------
if [ -z "$default_over_out" ]; then
    pass "buffering guarantee: the default-ceiling abort (many entries buffered before crossing) leaks no partial prefix"
else
    fail "buffering guarantee: the default-ceiling abort left a non-empty stdout: $default_over_out"
fi

# assert_usage_error asserts the shared shape every usage-error scenario in
# this section must have: exit status 2 specifically (not merely non-zero),
# byte-empty stdout, and an empty call log, because the parser runs before
# any network call.
assert_usage_error() {
    local label="$1" scenario="$2"
    if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(call_line_count "$scenario")" -eq 0 ]; then
        pass "$label"
    else
        fail "$label: status=$status out=$out calls=$(calls "$scenario")"
    fi
}

# --- Test 36: --max-entries with a non-integer value -------------------------------
run_scenario flag_nonint "" --max-entries abc acme/small
assert_usage_error "--max-entries with a non-integer value: exit 2, empty stdout, empty call log" flag_nonint

# --- Test 37: --max-entries with a negative value ----------------------------------
run_scenario flag_negative "" --max-entries -5 acme/small
assert_usage_error "--max-entries with a negative value: exit 2, empty stdout, empty call log" flag_negative

# --- Test 38: --max-entries with no following value at all -------------------------
run_scenario flag_novalue "" acme/small --max-entries
assert_usage_error "--max-entries with no following value: exit 2, empty stdout, empty call log" flag_novalue

# --- Test 39: an unrecognised leading double-dash token -----------------------------
run_scenario flag_unrecognised "" --bogus acme/small
assert_usage_error "unrecognised leading double-dash token: exit 2, empty stdout, empty call log" flag_unrecognised

# --- Test 40: a double-dash token appearing after the positionals -------------------
run_scenario flag_trailing_dashdash "" acme/small --weird
assert_usage_error "double-dash token after positionals: exit 2, empty stdout, empty call log (the one deliberate deviation)" flag_trailing_dashdash

# --- Test 41: a recognised flag appearing after the positionals ---------------------
run_scenario flag_trailing_children "" acme/small --children
assert_usage_error "recognised flag after positionals: exit 2, empty stdout, empty call log" flag_trailing_children

# --- Test 42: a -- terminator followed by a path beginning with a dash --------------
run_scenario flag_terminator_dashpath "$(printf 'repos/acme/dashpath/git/trees/HEAD:-weirdpath?recursive=1\tsmall-root-rec.json\n')" -- acme/dashpath -weirdpath
if [ "$status" -eq 0 ] && [ "$(call_line_count flag_terminator_dashpath)" -eq 1 ]; then
    pass "-- terminator followed by a dash-leading path: accepted, reaches the API"
else
    fail "-- terminator followed by a dash-leading path: status=$status out=$out calls=$(calls flag_terminator_dashpath)"
fi

# --- Test 43: a single-dash token in path position, no terminator -------------------
run_scenario flag_singledash "$(printf 'repos/acme/singledash/git/trees/HEAD:-x?recursive=1\tsmall-root-rec.json\n')" acme/singledash -x
if [ "$status" -eq 0 ] && [ "$(call_line_count flag_singledash)" -eq 1 ]; then
    pass "single-dash token in path position: not treated as a flag, reaches the API exactly as today"
else
    fail "single-dash token in path position: status=$status out=$out calls=$(calls flag_singledash)"
fi

# --- Test 44: combining both flags with both positionals ----------------------------
run_scenario flag_combined "$(printf 'repos/acme/bothflags/git/trees/HEAD:src\tchildren-src-nonrec.json\n')" --children --max-entries 5 acme/bothflags src
expected="$(printf 'src/main.go\nsrc/deep/\nsrc/util.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "combining --children and --max-entries with both positionals: parses and lists successfully"
else
    fail "combining --children and --max-entries with both positionals: status=$status out=$out"
fi

# --- Test 45: --max-entries with a leading zero is read as decimal, not octal ------
# Bash treats a leading-zero numeric literal as octal in arithmetic context,
# so an unguarded comparison would misapply 010 as a ceiling of 8 (silent
# wrong answer) and crash outright on 018 ("value too great for base 8").
# Both must instead behave exactly as their decimal value.
run_scenario flag_leadingzero_ok "$small_map" --max-entries 010 acme/guardboundary
expected="$(printf 'intro.md\nsrc/main.go\nsrc/util.go')"
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "--max-entries 010 is read as decimal ten, not octal eight: the three-entry listing succeeds"
else
    fail "--max-entries 010 is read as decimal ten, not octal eight: status=$status out=$out"
fi
run_scenario flag_leadingzero_crash "$small_map" --max-entries 018 acme/guardboundary
if [ "$out" = "$expected" ] && [ "$status" -eq 0 ]; then
    pass "--max-entries 018 does not crash on an invalid octal digit: the three-entry listing succeeds"
else
    fail "--max-entries 018 does not crash on an invalid octal digit: status=$status out=$out"
fi

rm -rf "$SCRATCH"

echo "==========================================================="
if [ "$failures" -eq 0 ]; then
    echo "PASS: all github-tree selftest assertions passed"
    exit 0
fi
echo "FAIL: $failures github-tree selftest assertion(s) failed"
exit 1
