#!/usr/bin/env bash
# github-read-selftest.sh is an offline harness (mirroring
# github-tree-selftest.sh's shape) that drives github-read.sh through a
# stub `curl` and a stub `gh` on PATH, with no network access at all. It
# asserts exact stdout bytes, exact argument vectors for both commands,
# exact call counts and ordering, and distinguishing stderr substrings.
#
# System `jq` is a dependency of THIS HARNESS ONLY, never of
# github-read.sh, which parses its `--jq` expression through `gh`'s own
# embedded gojq at run time -- the same accepted seam github-tree-selftest.sh
# documents.
#
# Portability envelope: the stub-on-PATH mechanism needs two extensionless
# executables with the exec bit set, so Linux and macOS are the asserted
# platforms. Windows Git Bash is expected to work but is not claimed;
# cmd.exe and PowerShell cannot run this harness or the stubs at all.
#
# NOT covered here (documented, not asserted -- manual checks): one live
# run against a public repository confirming the raw path is taken; one
# live run against a private repository confirming the fallback fires and
# succeeds; one live run against a directory path in a private repository;
# and whatever testdata/github-read/CAPTURE.md records as not exercised
# offline (symlink and submodule type-probe responses, and the
# contents-API size ceiling).
set -u

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLUGIN_ROOT="$SCRIPT_DIR/.."
GH_READ_SH="$SCRIPT_DIR/github-read.sh"
# Resolved once, up front, before the missing-gh test strips PATH for its
# own invocation: bash's temporary-assignment scoping (`VAR=val cmd`)
# applies the modified PATH to the search for `cmd` itself, so bare `bash`
# cannot be found by name once PATH has been emptied. Using this absolute
# path (which contains a slash, so execve needs no PATH search at all) is
# what lets that test actually exercise github-read.sh's own "gh not
# found" guard instead of failing before it even starts.
BASH_BIN="$(command -v bash)"
STUB_BIN="$SCRIPT_DIR/testdata/github-read/bin"
BODIES_DIR="$SCRIPT_DIR/testdata/github-read/bodies"

# .scratch/ is this repo's sanctioned scratch location (gitignored) --
# never a system temp directory, so a run leaves no trace outside the repo.
SCRATCH="$PLUGIN_ROOT/../../.scratch/github-read-selftest"

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

JQ_BIN="${GITHUB_READ_SELFTEST_JQ:-jq}"

require_jq() {
    if ! command -v "$JQ_BIN" >/dev/null 2>&1; then
        echo "github-read-selftest: '$JQ_BIN' not found on PATH — install jq to run this harness" >&2
        return 1
    fi
    return 0
}

require_jq || exit 1

rm -rf "$SCRATCH"
mkdir -p "$SCRATCH"

# build_curl_free_stub_dir builds, under the harness scratch, a directory
# containing a copy of the stub `gh` and nothing else, and prints its path.
# github-tree-selftest.sh's own missing-binary trick -- emptying PATH
# entirely -- cannot serve the curl-absent scenario, because that scenario
# needs `gh` to still resolve while `curl` does not, and an emptied PATH
# hides both. Pointing PATH at a directory holding only the stub `gh`, with
# no other directory behind it, is what makes the absence real rather than
# assumed.
build_curl_free_stub_dir() {
    local dir="$SCRATCH/curl-free-bin"
    rm -rf "$dir"
    mkdir -p "$dir"
    cp "$STUB_BIN/gh" "$dir/gh"
    chmod +x "$dir/gh"
    echo "$dir"
}

# run_scenario_with_stub_dir writes <curl-map-content> and <gh-map-content>
# to the scenario's own scratch directory, truncates both stubs' call
# logs, points PATH at <stub-dir> (prepended) and TMPDIR at the scenario's
# own scratch directory (which is what makes the temp-file-cleanup
# assertions in the validation section possible), runs github-read.sh with
# the remaining arguments, and captures stdout, stderr, and exit status
# into the shell variables out/err/status. Capturing stdout and stderr
# separately is mandatory: many assertions below turn on stdout being
# byte-empty while stderr is not.
run_scenario_with_stub_dir() {
    local name="$1" stub_dir="$2" curl_map="$3" gh_map="$4"
    shift 4
    local dir="$SCRATCH/$name"
    mkdir -p "$dir"
    printf '%s' "$curl_map" >"$dir/curl_map.tsv"
    printf '%s' "$gh_map" >"$dir/gh_map.tsv"
    : >"$dir/curl_calls.log"
    : >"$dir/gh_calls.log"
    PATH="$stub_dir:$PATH" \
        TMPDIR="$dir" \
        CURL_STUB_MAP="$dir/curl_map.tsv" \
        CURL_STUB_BODIES="$BODIES_DIR" \
        CURL_STUB_LOG="$dir/curl_calls.log" \
        GH_STUB_MAP="$dir/gh_map.tsv" \
        GH_STUB_BODIES="$BODIES_DIR" \
        GH_STUB_LOG="$dir/gh_calls.log" \
        bash "$GH_READ_SH" "$@" >"$dir/stdout" 2>"$dir/stderr"
    status=$?
    out="$(cat "$dir/stdout")"
    err="$(cat "$dir/stderr")"
}

# run_scenario is the default form of the runner above, pointed at the
# normal stub directory -- the parameter every scenario but the
# curl-absent one uses.
run_scenario() {
    local name="$1" curl_map="$2" gh_map="$3"
    shift 3
    run_scenario_with_stub_dir "$name" "$STUB_BIN" "$curl_map" "$gh_map" "$@"
}

# curl_calls / gh_calls print a scenario's call log verbatim.
curl_calls() {
    cat "$SCRATCH/$1/curl_calls.log" 2>/dev/null
}

gh_calls() {
    cat "$SCRATCH/$1/gh_calls.log" 2>/dev/null
}

# curl_call_line_count / gh_call_line_count print the number of lines in a
# scenario's call log for that stub.
curl_call_line_count() {
    local logfile="$SCRATCH/$1/curl_calls.log"
    [ -f "$logfile" ] || { echo 0; return; }
    wc -l <"$logfile" | tr -d ' '
}

gh_call_line_count() {
    local logfile="$SCRATCH/$1/gh_calls.log"
    [ -f "$logfile" ] || { echo 0; return; }
    wc -l <"$logfile" | tr -d ' '
}

echo "=== github-read selftest: offline stub-curl/stub-gh harness ==="

# --- Test 1: the preference-order proof --------------------------------
# The single most load-bearing assertion in this harness: a successful raw
# read makes zero gh calls. That is the measured win the whole task exists
# to capture, and it is the one assertion that fails if the fallback order
# is ever inverted.
run_scenario pref_order "$(printf '0\tplain.txt')" "" acme/pref src/a.txt
if cmp -s "$SCRATCH/pref_order/stdout" "$BODIES_DIR/plain.txt" && [ "$status" -eq 0 ]; then
    pass "preference order: stdout byte-identical to the fixture, exit 0"
else
    fail "preference order: status=$status"
fi
if [ "$(curl_call_line_count pref_order)" -eq 1 ]; then
    pass "preference order: exactly one curl call"
else
    fail "preference order: curl call log has $(curl_call_line_count pref_order) lines, expected 1: $(curl_calls pref_order)"
fi
if [ "$(gh_call_line_count pref_order)" -eq 0 ]; then
    pass "preference order: zero gh calls on a successful raw read"
else
    fail "preference order: gh was called despite a successful raw read: $(gh_calls pref_order)"
fi
case "$(curl_calls pref_order)" in
*"https://raw.githubusercontent.com/acme/pref/HEAD/src/a.txt"*) pass "preference order: raw URL carries the repo, the literal HEAD, and the path" ;;
*) fail "preference order: raw URL malformed: $(curl_calls pref_order)" ;;
esac

# --- Test 2: the argument-vector proof -----------------------------------
# Splitting on whitespace is safe here because none of this vector's
# tokens (including the URL, whose character set is validated before this
# call is ever made) can contain a space.
read -r -a vec <<<"$(curl_calls pref_order)"
if [ "${vec[0]:-}" = "-s" ] && [ "${vec[1]:-}" = "-f" ] && [ "${vec[2]:-}" = "-L" ] \
    && [ "${vec[3]:-}" = "--connect-timeout" ] && [ "${vec[4]:-}" = "5" ] \
    && [ "${vec[5]:-}" = "--max-time" ] && [ "${vec[6]:-}" = "30" ] \
    && [ "${vec[7]:-}" = "-o" ] \
    && [ "${vec[9]:-}" = "https://raw.githubusercontent.com/acme/pref/HEAD/src/a.txt" ] \
    && [ "${#vec[@]}" -eq 10 ]; then
    pass "argument vector: -s -f -L --connect-timeout 5 --max-time 30 -o <tmp> <url>, in order, no other flags"
else
    fail "argument vector: $(curl_calls pref_order)"
fi
if [ "${vec[1]:-}" = "-f" ]; then
    pass "argument vector: -f present specifically (its absence would degrade a 404 into fake file content)"
else
    fail "argument vector: -f missing"
fi

# --- Test 3: the trigger is curl's exit status, not a parsed code -------
run_scenario trigger_timeout "$(printf '28\t')" "$(printf 'probe\trepos/acme/trig/contents/x.txt\tprobe-file.json\t\t\ncontent\trepos/acme/trig/contents/x.txt\tplain.txt\t\t\n')" acme/trig x.txt
if cmp -s "$SCRATCH/trigger_timeout/stdout" "$BODIES_DIR/plain.txt" && [ "$status" -eq 0 ]; then
    pass "trigger is exit status: a timeout exit (28) falls through to a succeeding fallback"
else
    fail "trigger is exit status (timeout): status=$status out=$out"
fi
run_scenario trigger_refused "$(printf '7\t')" "$(printf 'probe\trepos/acme/trig2/contents/x.txt\tprobe-file.json\t\t\ncontent\trepos/acme/trig2/contents/x.txt\tplain.txt\t\t\n')" acme/trig2 x.txt
if cmp -s "$SCRATCH/trigger_refused/stdout" "$BODIES_DIR/plain.txt" && [ "$status" -eq 0 ]; then
    pass "trigger is exit status: a connection-refused exit (7) falls through to a succeeding fallback"
else
    fail "trigger is exit status (connection refused): status=$status out=$out"
fi

# --- Test 4: the not-found regression test -------------------------------
# The stub writes nothing to its output file and exits with the status the
# -f flag would produce for a 404 (22); the raw-attempt loop's own
# no-body-inspection contract must never write curl's would-be "404: Not
# Found" text (which the -f flag prevents from reaching the output file in
# the first place) as though it were file content.
run_scenario not_found "$(printf '22\t')" "$(printf 'probe\trepos/acme/nf/contents/gone.txt\terror-404.json\t404\t\n')" acme/nf gone.txt
case "$out" in
*"Not Found"*) fail "not-found regression: stdout carries not-found text: $out" ;;
*) pass "not-found regression: stdout carries no not-found text" ;;
esac
if [ "$status" -ne 0 ]; then
    pass "not-found regression: non-zero exit"
else
    fail "not-found regression: exit 0 unexpected"
fi

# --- Test 5: the no-partial-prefix proof ----------------------------------
# This assertion would fail against a stream-to-stdout implementation and
# is what pins the temp-file buffering decision: a partial body written by
# a dying connection must never appear ahead of the fallback's own bytes.
run_scenario no_partial "$(printf '18\tpartial-decoy.txt')" "$(printf 'probe\trepos/acme/partial/contents/x.txt\tprobe-file.json\t\t\ncontent\trepos/acme/partial/contents/x.txt\tplain.txt\t\t\n')" acme/partial x.txt
if cmp -s "$SCRATCH/no_partial/stdout" "$BODIES_DIR/plain.txt"; then
    pass "no-partial-prefix: stdout is exactly the fallback's bytes, no partial prefix in front of them"
else
    fail "no-partial-prefix: stdout does not match the fixture exactly (a partial prefix or trailing bytes leaked)"
fi

# --- Test 6: curl absent from PATH ----------------------------------------
curl_free_dir="$(build_curl_free_stub_dir)"
run_scenario_with_stub_dir curl_absent "$curl_free_dir" "" "$(printf 'probe\trepos/acme/nocurl/contents/x.txt\tprobe-file.json\t\t\ncontent\trepos/acme/nocurl/contents/x.txt\tplain.txt\t\t\n')" acme/nocurl x.txt
if cmp -s "$SCRATCH/curl_absent/stdout" "$BODIES_DIR/plain.txt" && [ "$status" -eq 0 ]; then
    pass "curl absent from PATH: script goes straight to gh api and still produces correct stdout"
else
    fail "curl absent from PATH: status=$status out=$out"
fi
if [ "$(curl_call_line_count curl_absent)" -eq 0 ]; then
    pass "curl absent from PATH: zero curl calls logged (curl genuinely cannot resolve)"
else
    fail "curl absent from PATH: a curl call was logged despite curl's absence: $(curl_calls curl_absent)"
fi
case "$err" in
*curl*) fail "curl absent from PATH: stderr mentions curl: $err" ;;
*) pass "curl absent from PATH: stderr says nothing about curl" ;;
esac

# --- Test 7: the happy fallback -------------------------------------------
run_scenario fallback_happy "$(printf '7\t')" "$(printf 'probe\trepos/acme/happy/contents/x.txt\tprobe-file.json\t\t\ncontent\trepos/acme/happy/contents/x.txt\tplain.txt\t\t\n')" acme/happy x.txt
if cmp -s "$SCRATCH/fallback_happy/stdout" "$BODIES_DIR/plain.txt" && [ "$status" -eq 0 ]; then
    pass "happy fallback: correct stdout, exit 0"
else
    fail "happy fallback: status=$status out=$out"
fi
if [ "$(curl_call_line_count fallback_happy)" -eq 1 ]; then
    pass "happy fallback: exactly one curl call"
else
    fail "happy fallback: curl call log has $(curl_call_line_count fallback_happy) lines"
fi
if [ "$(gh_call_line_count fallback_happy)" -eq 2 ]; then
    pass "happy fallback: exactly two gh calls"
else
    fail "happy fallback: gh call log has $(gh_call_line_count fallback_happy) lines, expected 2: $(gh_calls fallback_happy)"
fi
gh_line1="$(gh_calls fallback_happy | sed -n '1p')"
gh_line2="$(gh_calls fallback_happy | sed -n '2p')"
if [ "$gh_line1" = "api repos/acme/happy/contents/x.txt --jq if type==\"array\" then \"dir\" else .type end" ]; then
    pass "happy fallback: first gh call is the probe, full argument vector including the jq expression"
else
    fail "happy fallback: first gh call vector unexpected: $gh_line1"
fi
if [ "$gh_line2" = "api repos/acme/happy/contents/x.txt -H Accept: application/vnd.github.raw" ]; then
    pass "happy fallback: second gh call is the content fetch, full argument vector including the raw-media header"
else
    fail "happy fallback: second gh call vector unexpected: $gh_line2"
fi

# --- Test 8: the directory rejection ---------------------------------------
run_scenario fallback_dir "$(printf '7\t')" "$(printf 'probe\trepos/acme/dirrej/contents/adir\tprobe-dir.json\t\t\n')" acme/dirrej adir
if [ "$status" -eq 1 ] && [ -z "$out" ]; then
    pass "directory rejection: exit status 1, byte-empty stdout"
else
    fail "directory rejection: status=$status out=$out"
fi
err_lines="$(printf '%s\n' "$err" | grep -c .)"
if [ "$err_lines" -eq 1 ] && [[ "$err" == *adir* ]] && [[ "$err" == *"github-tree.sh"* ]] && [[ "$err" == *"--children"* ]]; then
    pass "directory rejection: exactly one stderr line naming the path and the sibling tree script's children mode"
else
    fail "directory rejection: err=$err (lines=$err_lines)"
fi
if [ "$(gh_call_line_count fallback_dir)" -eq 1 ]; then
    pass "directory rejection: exactly one gh call, proving the content fetch never ran"
else
    fail "directory rejection: gh call log has $(gh_call_line_count fallback_dir) lines: $(gh_calls fallback_dir)"
fi

# --- Test 9: the symlink and submodule rejections ---------------------------
run_scenario fallback_symlink "$(printf '7\t')" "$(printf 'probe\trepos/acme/linkrej/contents/l\tprobe-symlink.json\t\t\n')" acme/linkrej l
if [ "$status" -ne 0 ] && [ -z "$out" ] && [[ "$err" == *symlink* ]]; then
    pass "symlink rejection: non-zero exit, byte-empty stdout, observed type 'symlink' named in stderr"
else
    fail "symlink rejection: status=$status out=$out err=$err"
fi
run_scenario fallback_submodule "$(printf '7\t')" "$(printf 'probe\trepos/acme/subrej/contents/s\tprobe-submodule.json\t\t\n')" acme/subrej s
if [ "$status" -ne 0 ] && [ -z "$out" ] && [[ "$err" == *submodule* ]]; then
    pass "submodule rejection: non-zero exit, byte-empty stdout, observed type 'submodule' named in stderr"
else
    fail "submodule rejection: status=$status out=$out err=$err"
fi

# --- Test 10: the three authenticated failures -------------------------------
run_scenario fallback_401 "$(printf '7\t')" "$(printf 'probe\trepos/acme/e401/contents/x.txt\terror-401.json\t401\t\n')" acme/e401 x.txt
if [ "$status" -ne 0 ] && [ -z "$out" ] && [ "$(printf '%s\n' "$err" | grep -c .)" -eq 1 ] \
    && [[ "$err" == *"not authenticated"* ]] && [[ "$err" != *raw* ]] && [ "$(gh_call_line_count fallback_401)" -eq 1 ]; then
    pass "HTTP 401: one stderr line, no mention of the raw attempt, second gh call never happened"
else
    fail "HTTP 401: status=$status out=$out err=$err gh_calls=$(gh_call_line_count fallback_401)"
fi
run_scenario fallback_403 "$(printf '7\t')" "$(printf 'probe\trepos/acme/e403/contents/x.txt\terror-403.json\t403\t\n')" acme/e403 x.txt
if [ "$status" -ne 0 ] && [ -z "$out" ] && [ "$(printf '%s\n' "$err" | grep -c .)" -eq 1 ] \
    && [[ "$err" == *"rate limited"* ]] && [[ "$err" != *raw* ]] && [ "$(gh_call_line_count fallback_403)" -eq 1 ]; then
    pass "HTTP 403: one stderr line, no mention of the raw attempt, second gh call never happened"
else
    fail "HTTP 403: status=$status out=$out err=$err gh_calls=$(gh_call_line_count fallback_403)"
fi
run_scenario fallback_404 "$(printf '7\t')" "$(printf 'probe\trepos/acme/e404/contents/x.txt\terror-404.json\t404\terror-404.stderr\n')" acme/e404 x.txt
if [ "$status" -ne 0 ] && [ -z "$out" ] && [ "$(printf '%s\n' "$err" | grep -c .)" -eq 1 ] \
    && [[ "$err" == *"not found"* ]] && [[ "$err" != *raw* ]] && [ "$(gh_call_line_count fallback_404)" -eq 1 ]; then
    pass "HTTP 404: one stderr line, no mention of the raw attempt, second gh call never happened"
else
    fail "HTTP 404: status=$status out=$out err=$err gh_calls=$(gh_call_line_count fallback_404)"
fi

# --- Test 11: the three status-extraction sources, in isolation -------------
run_scenario status_from_body "$(printf '7\t')" "$(printf 'probe\trepos/acme/sbody/contents/x.txt\terror-404.json\t404\t\n')" acme/sbody x.txt
if [[ "$err" == *"404"* ]]; then
    pass "status extraction: body-only fixture (no stderr fixture) yields the code from the body"
else
    fail "status extraction from body: err=$err"
fi
run_scenario status_from_stderr "$(printf '7\t')" "$(printf 'probe\trepos/acme/sstderr/contents/x.txt\terror-nostatus.json\t500\terror-404.stderr\n')" acme/sstderr x.txt
if [[ "$err" == *"404"* ]]; then
    pass "status extraction: no-status body plus a parenthesised stderr code yields the code from stderr"
else
    fail "status extraction from stderr: err=$err"
fi
run_scenario status_generic "$(printf '7\t')" "$(printf 'probe\trepos/acme/sgeneric/contents/x.txt\terror-multiline.json\t500\t\n')" acme/sgeneric x.txt
err_lines_generic="$(printf '%s\n' "$err" | grep -c .)"
if [[ "$err" == *"repos/acme/sgeneric/contents/x.txt"* ]] && [[ "$err" == *"exit 1"* ]] && [[ "$err" == *"Server Error"* ]] && [ "$err_lines_generic" -eq 1 ]; then
    pass "status extraction: neither source yields a code -- generic form names the endpoint and exit status, still one physical stderr line despite the multi-line body"
else
    fail "status extraction generic form: err=$err (lines=$err_lines_generic)"
fi

# --- Test 12: byte-empty stdout across every fallback-failure scenario ------
for scenario in fallback_dir fallback_symlink fallback_submodule fallback_401 fallback_403 fallback_404 status_from_body status_from_stderr status_generic; do
    if [ -s "$SCRATCH/$scenario/stdout" ]; then
        fail "byte-empty stdout on failure: $scenario left non-empty stdout (the failed fallback's error body reached it)"
    else
        pass "byte-empty stdout on failure: $scenario"
    fi
done

# --- Test 13: gh missing from PATH -------------------------------------------
PATH="" "$BASH_BIN" "$GH_READ_SH" acme/nogh x.txt >"$SCRATCH/nogh.stdout" 2>"$SCRATCH/nogh.stderr"
status=$?
out="$(cat "$SCRATCH/nogh.stdout")"
err="$(cat "$SCRATCH/nogh.stderr")"
if [ -z "$out" ] && [ "$status" -eq 1 ] && [[ "$err" == *gh* ]]; then
    pass "gh missing from PATH: exit 1, byte-empty stdout, stderr mentions gh, no network call happened"
else
    fail "gh missing from PATH: status=$status out=$out err=$err"
fi

# --- Test 14: path validation ------------------------------------------------
run_scenario badchar "" "" acme/small "a#b"
if [ -z "$out" ] && [ "$status" -eq 1 ] && [ "$(curl_call_line_count badchar)" -eq 0 ] && [ "$(gh_call_line_count badchar)" -eq 0 ] && [[ "$err" == *"#"* ]]; then
    pass "path with a '#': rejected locally, zero calls to either command, stderr names the '#'"
else
    fail "path with a '#': status=$status out=$out err=$err"
fi
run_scenario badchar_accented "" "" acme/small "naïve"
if [ -z "$out" ] && [ "$status" -eq 1 ] && [ "$(curl_call_line_count badchar_accented)" -eq 0 ] && [ "$(gh_call_line_count badchar_accented)" -eq 0 ] && [[ "$err" == *"ï"* ]]; then
    pass "path with 'naïve': rejected locally (the reproduced glob-vs-regex failure this technique exists to prevent), stderr names 'ï' whole"
else
    fail "path with 'naïve': status=$status out=$out err=$err"
fi

# --- Test 15: path normalisation ----------------------------------------------
# strip_temp_path blanks out the -o flag's value (the 9th whitespace-
# separated field of a logged curl call) before comparison, because that
# value is a scenario-specific mktemp path that necessarily differs across
# separately-run scenarios even for byte-identical invocations otherwise --
# not part of what "byte-identical call logs" is asserting here.
strip_temp_path() {
    local line="$1"
    read -r -a a <<<"$line"
    a[8]="<tmp>"
    echo "${a[*]}"
}
norm_curl_map="$(printf '0\tplain.txt')"
run_scenario norm1 "$norm_curl_map" "" acme/norm src/a.txt
calls1="$(strip_temp_path "$(curl_calls norm1)")"
run_scenario norm2 "$norm_curl_map" "" acme/norm /src/a.txt
calls2="$(strip_temp_path "$(curl_calls norm2)")"
run_scenario norm3 "$norm_curl_map" "" acme/norm src/a.txt/
calls3="$(strip_temp_path "$(curl_calls norm3)")"
if [ "$calls1" = "$calls2" ] && [ "$calls2" = "$calls3" ]; then
    pass "path normalisation: bare, leading-slash, and trailing-slash paths produce byte-identical call logs"
else
    fail "path normalisation: calls1=$calls1 calls2=$calls2 calls3=$calls3"
fi

# --- Test 16: empty and slash-only paths are usage errors --------------------
run_scenario emptypath "" "" acme/small ""
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count emptypath)" -eq 0 ] && [ "$(gh_call_line_count emptypath)" -eq 0 ]; then
    pass "empty path: usage error, exit 2, no calls"
else
    fail "empty path: status=$status out=$out"
fi
run_scenario slashonlypath "" "" acme/small "/"
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count slashonlypath)" -eq 0 ] && [ "$(gh_call_line_count slashonlypath)" -eq 0 ]; then
    pass "slash-only path: usage error, exit 2, no calls"
else
    fail "slash-only path: status=$status out=$out"
fi

# --- Test 17: argument count ---------------------------------------------------
run_scenario argcount_zero "" ""
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count argcount_zero)" -eq 0 ] && [ "$(gh_call_line_count argcount_zero)" -eq 0 ]; then
    pass "zero positionals: usage error, exit 2, no calls"
else
    fail "zero positionals: status=$status out=$out"
fi
run_scenario argcount_one "" "" acme/small
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count argcount_one)" -eq 0 ] && [ "$(gh_call_line_count argcount_one)" -eq 0 ]; then
    pass "one positional: usage error, exit 2, no calls"
else
    fail "one positional: status=$status out=$out"
fi
run_scenario argcount_three "" "" -- acme/small x.txt extra
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count argcount_three)" -eq 0 ] && [ "$(gh_call_line_count argcount_three)" -eq 0 ]; then
    pass "three positionals after a terminator: usage error, exit 2, no calls"
else
    fail "three positionals after a terminator: status=$status out=$out"
fi
run_scenario argcount_terminator_dashpath "$(printf '0\tplain.txt')" "" -- acme/small --weird-path
if [ "$status" -eq 0 ] && [ "$(curl_call_line_count argcount_terminator_dashpath)" -eq 1 ]; then
    pass "terminator plus a doubly-dashed path: a legitimate two-positional call, reaches the API"
else
    fail "terminator plus a doubly-dashed path: status=$status out=$out calls=$(curl_calls argcount_terminator_dashpath)"
fi

# --- Test 18: a double-dash token without a terminator -------------------------
run_scenario dashdash_noterm "" "" acme/small --weird
if [ -z "$out" ] && [ "$status" -eq 2 ] && [ "$(curl_call_line_count dashdash_noterm)" -eq 0 ] && [ "$(gh_call_line_count dashdash_noterm)" -eq 0 ]; then
    pass "a positional beginning with two dashes, no terminator: usage error, exit 2, empty call logs (matches github-tree.sh's own behaviour for the same token)"
else
    fail "double-dash without a terminator: status=$status out=$out"
fi

# --- Test 19: a malformed owner/repo reference ----------------------------------
run_scenario badslug "" "" notaslug x.txt
if [ -z "$out" ] && [ "$status" -ne 0 ] && [ "$(curl_call_line_count badslug)" -eq 0 ] && [ "$(gh_call_line_count badslug)" -eq 0 ]; then
    pass "malformed owner/repo reference: rejected locally, no calls to either command"
else
    fail "malformed owner/repo reference: status=$status out=$out"
fi

# --- Test 20: byte fidelity -----------------------------------------------------
run_scenario fidelity_plain "$(printf '0\tplain.txt')" "" acme/fid plain.txt
if cmp -s "$SCRATCH/fidelity_plain/stdout" "$BODIES_DIR/plain.txt"; then
    pass "byte fidelity: ordinary fixture, stdout byte-identical (no added trailing newline, no banner, no filename)"
else
    fail "byte fidelity: ordinary fixture mismatch"
fi
run_scenario fidelity_nul "$(printf '0\twithnul.bin')" "" acme/fid withnul.bin
if cmp -s "$SCRATCH/fidelity_nul/stdout" "$BODIES_DIR/withnul.bin"; then
    pass "byte fidelity: NUL-byte fixture, stdout byte-identical (no silent truncation or drop of the NUL byte)"
else
    fail "byte fidelity: NUL-byte fixture mismatch"
fi
run_scenario fidelity_zero "$(printf '0\tzero.txt')" "" acme/fid zero.txt
if [ "$status" -eq 0 ] && [ -z "$out" ]; then
    pass "byte fidelity: zero-byte fixture, exit 0 with byte-empty stdout -- success distinguished from failure only by the exit code"
else
    fail "byte fidelity: zero-byte fixture status=$status out=$out"
fi

# --- Test 21: cleanup -------------------------------------------------------------
# temp_leftover_count counts files matching mktemp's default naming pattern
# left behind in a scenario's own scratch directory (which TMPDIR was
# pointed at), proving the EXIT trap removed both of github-read.sh's own
# temp files on every exit path, success or failure.
temp_leftover_count() {
    local dir="$SCRATCH/$1"
    find "$dir" -maxdepth 1 -name 'tmp.*' 2>/dev/null | wc -l | tr -d ' '
}
run_scenario cleanup_success "$(printf '0\tplain.txt')" "" acme/cleanup1 plain.txt
if [ "$(temp_leftover_count cleanup_success)" -eq 0 ]; then
    pass "cleanup: after a successful read, the scratch temp directory holds no leftover mktemp file"
else
    fail "cleanup: leftover temp file(s) after a successful read"
fi
run_scenario cleanup_bothfail "$(printf '7\t')" "$(printf 'probe\trepos/acme/cleanup2/contents/x.txt\terror-404.json\t404\t\n')" acme/cleanup2 x.txt
if [ "$(temp_leftover_count cleanup_bothfail)" -eq 0 ]; then
    pass "cleanup: after a read where both backends failed, the scratch temp directory holds no leftover mktemp file"
else
    fail "cleanup: leftover temp file(s) after a both-backends-failed read"
fi

rm -rf "$SCRATCH"

echo "==========================================================="
if [ "$failures" -eq 0 ]; then
    echo "PASS: all github-read selftest assertions passed"
    exit 0
fi
echo "FAIL: $failures github-read selftest assertion(s) failed"
exit 1
