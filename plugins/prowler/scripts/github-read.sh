#!/usr/bin/env bash
# github-read.sh reads exactly one file from a GitHub repository and writes
# its content verbatim to stdout and nothing else. It prefers a direct
# https://raw.githubusercontent.com/ fetch and falls back to `gh api` only
# when the raw fetch fails -- the raw host is measurably faster (no
# authenticated-API round trip) and the fallback exists purely to cover
# what raw cannot: private repositories, and the rare host-level hiccup.
#
# There are deliberately no retries and no backoff anywhere in this script,
# the same policy github-tree.sh documents. The `--connect-timeout` and
# `--max-time` bounds given to curl are not retries either -- they exist
# only to turn a hung raw request into one clean non-zero exit within a
# bounded time, so the single `gh api` fallback attempt actually gets a
# turn instead of the script hanging forever on a dead connection.
#
# Like github-tree.sh, this script reads no file inside the plugin -- it
# takes an owner/repo and a path and calls `curl`/`gh`, nothing else -- so
# it does not self-locate a SCRIPT_DIR/PLUGIN_ROOT.
#
# This script reads exactly one file per invocation: stdout carries that
# file's content verbatim and nothing else, every diagnostic goes to
# stderr, and a read is pinned to `HEAD` -- there is no ref argument, and
# no way to read anything but the default branch's current content.
set -u

# die prints one message to stderr and exits 1. It is not used for the
# usage error, which is the one case that exits 2 instead.
die() {
    echo "$1" >&2
    exit 1
}

# usage prints the usage line to stderr and exits 2. It is deliberately not
# routed through die, which exits 1 -- exit 2 is reserved for malformed
# invocations, exit 1 for every operational failure.
usage() {
    echo "github-read: usage: github-read.sh <owner/repo> <path>" >&2
    exit 2
}

# --- Prerequisite and argument handling, in this exact order, so that a --
# --- rejection never reaches the network. --------------------------------

if ! command -v gh >/dev/null 2>&1; then
    die "github-read: gh not found on PATH — install the GitHub CLI and authenticate it (gh auth login)"
fi

# `curl` is not checked here: its absence is never an error, only a loss of
# the fast path (see the raw-attempt section below).

# Parse arguments with the same loop shape github-tree.sh uses, minus the
# two flags this script does not have: a `--` terminator is honoured at any
# position and consumed, every token after it is a positional unexamined,
# and before it any token beginning with two dashes is a usage error while
# every other token (including a single-dash token, which is never treated
# as a flag) is a positional.
args=()
terminated=0
while [ "$#" -gt 0 ]; do
    if [ "$terminated" -eq 1 ]; then
        args+=("$1")
        shift
        continue
    fi
    case "$1" in
    --)
        terminated=1
        shift
        ;;
    --*)
        usage
        ;;
    *)
        args+=("$1")
        shift
        ;;
    esac
done

# The count is checked on the post-terminator positional list, so a
# terminator plus a doubly-dashed path is a legitimate two-positional call
# rather than a count error.
[ "${#args[@]}" -eq 2 ] || usage

REPO="${args[0]}"
RAW_PATH="${args[1]}"

# Copied verbatim from github-tree.sh, including its bracket-range regex
# form: the collation looseness this form carries under some locales is an
# accepted property there, and divergence between the two scripts' slug
# checks would be the worse outcome.
if ! [[ "$REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    die "github-read: '$REPO' is not a valid <owner>/<repo> reference"
fi

# Normalize the path: strip every leading and trailing '/'.
path="$RAW_PATH"
while [[ "$path" == /* ]]; do path="${path#/}"; done
while [[ "$path" == */ ]]; do path="${path%/}"; done

# Unlike github-tree.sh, an empty path after normalisation is a usage error
# rather than a whole-repo listing: there is no such thing as reading an
# entire repository as one file.
if [ -z "$path" ]; then
    usage
fi

# Validate by deleting every accepted character from a copy of the path and
# checking what remains, rather than testing the whole string against
# `[[ =~ ^[A-Za-z0-9._/-]+$ ]]` or indexing it byte by byte to find "the
# first offender".
#
# Regex bracket ranges like A-Z are collation-ordered, not codepoint-
# ordered: under a UTF-8 locale (this box's default), glibc's regex engine
# treats accented letters such as 'ï' as falling inside that collation
# range, so `[[ "naïve" =~ ^[A-Za-z0-9._/-]+$ ]]` wrongly matches -- a real,
# reproduced failure mode, not a hypothetical one. Bash's glob-pattern
# matching (which parameter-expansion substitution uses) does not have this
# problem, so it is used for both detection and the offending-character
# report.
#
# Byte indexing has a separate, independent problem: it is byte-oriented
# under a C/POSIX locale, which minimal shells and CI images routinely run
# with and which this script deliberately does not pin, so indexing would
# slice a multi-byte UTF-8 character into single invalid bytes and report
# one of them as "the character". The glob substitution below is immune to
# that too: the accepted set is pure ASCII, so a UTF-8 continuation byte is
# never in it and no multi-byte sequence can be split -- the remainder
# always holds each offending character whole. Reporting the full remaining
# set rather than just the first offender is the same choice made for the
# same reason: there is no first-offender extraction that is byte-safe
# without re-introducing the indexing problem.
#
# The accepted character set is a subset of URL-safe characters, which is
# what makes URL-encoding unnecessary when the path is interpolated
# directly into the raw URL below.
offending="${path//[A-Za-z0-9._\/-]/}"
if [ -n "$offending" ]; then
    die "github-read: path '$path' contains unsupported character(s) '$offending' — only [A-Za-z0-9._/-] is accepted"
fi

# Two temp files, created unconditionally so neither is unset under `set
# -u`, and a single EXIT trap removing both, armed immediately after both
# exist so no exit path -- including a `die` before either is used -- ever
# leaks either one.
BODY_FILE="$(mktemp)"
STDERR_FILE="$(mktemp)"
trap 'rm -f "$BODY_FILE" "$STDERR_FILE"' EXIT

# --- Raw attempt: the fast path ------------------------------------------
#
# A missing `curl` costs speed, not capability: fall straight through to
# the `gh api` fallback with no error and nothing written to stderr.
# Warning on every read would be noise the caller cannot act on, since the
# fallback covers this case completely.
if command -v curl >/dev/null 2>&1; then
    raw_url="https://raw.githubusercontent.com/$REPO/HEAD/$path"

    # Exactly this argument vector, no other flags:
    #   -f  turns every response at or above 400 into a non-zero exit with
    #       an empty output file, so curl's exit status alone is a
    #       sufficient failure signal and no HTTP status needs capturing
    #       or parsing. Without it a plain request answers a 404 with exit
    #       0 and a "404: Not Found" body that would be emitted as if it
    #       were the file's content.
    #   -s  (rather than -sS) is required because this script never
    #       reports the raw attempt's failure -- curl must write no
    #       progress or error text to stderr either.
    #   -L  follows a redirect the raw host may answer with; an unfollowed
    #       301 would be a spurious fallback.
    #   --connect-timeout 5 / --max-time 30 bound a hung request so it
    #       becomes one clean non-zero exit that hands off to the single
    #       `gh api` attempt exactly once. These are bounds, not retries.
    #   -o "$BODY_FILE" writes the body to the temp file rather than
    #       streaming it to stdout: streaming would leave a truncated
    #       prefix on stdout if the connection died mid-body after a 200,
    #       and the fallback would then append a second copy behind it.
    #       Command substitution is avoided for the same call for a
    #       different reason -- it strips every trailing newline and
    #       silently drops NUL bytes, corrupting byte fidelity on every
    #       read rather than only on failure.
    if curl -s -f -L --connect-timeout 5 --max-time 30 -o "$BODY_FILE" "$raw_url"; then
        cat "$BODY_FILE"
        exit 0
    fi
    # Failure is defined as curl's exit status being non-zero and nothing
    # else: no HTTP status is captured, parsed, or branched on, and body
    # emptiness is never the signal, because an empty file that read
    # successfully (the zero-byte-file case) is a valid outcome the
    # harness asserts explicitly. Fall through to the gh api fallback.
fi

# --- gh api fallback: type probe, raw-Accept fetch, and diagnosis -------
#
# Reached whenever the raw attempt above did not already exit. Both calls
# below are paid only once the raw attempt has already failed, which is the
# rare and far slower path.
#
# diagnose <endpoint> <exit-status> derives the HTTP status of whichever
# call just failed and `die`s with the matching message. The order is
# deliberate: the body is checked first, because GitHub answers a non-2xx
# with a JSON error body carrying a `status` field whatever media type was
# requested, and `gh` writes that body to stdout -- the same `status`
# pattern github-tree.sh already uses. The stderr pass runs second, not
# first, because its message text is a CLI presentation string, not an API
# contract, and is the more likely of the two to change between releases.
# If neither source yields a code, the generic form names the endpoint, the
# exit status, and the body, with the body collapsed to one physical line
# the way github-tree.sh already collapses it, keeping the one-stderr-line
# contract literally.
diagnose() {
    local endpoint="$1" status="$2" body http

    body="$(cat "$BODY_FILE" 2>/dev/null)"
    http=""
    if [[ "$body" =~ \"status\"[[:space:]]*:[[:space:]]*\"?([0-9]{3})\"? ]]; then
        http="${BASH_REMATCH[1]}"
    elif [[ "$(cat "$STDERR_FILE" 2>/dev/null)" =~ \(HTTP\ ([0-9]{3})\) ]]; then
        http="${BASH_REMATCH[1]}"
    fi

    if [ "$http" = "401" ]; then
        die "github-read: repos/$REPO — not authenticated (HTTP 401); run 'gh auth login'"
    elif [ "$http" = "403" ]; then
        die "github-read: repos/$REPO — rate limited or access denied (HTTP 403)"
    elif [ "$http" = "404" ]; then
        die "github-read: repos/$REPO — path '$path' not found (HTTP 404)"
    else
        local collapsed="${body//$'\n'/ }"
        die "github-read: gh api $endpoint failed (exit $status): $collapsed"
    fi
}

# The type probe: one jq expression answers "dir" for a JSON-array
# response and the response's own `type` field otherwise, so one expression
# covers both shapes and no runtime `jq` is ever invoked -- every JSON
# field here is extracted through `gh api --jq`, which uses `gh`'s embedded
# engine, never a system `jq`.
#
# The probe exists because the contents endpoint answers a directory with
# HTTP 200 and a JSON listing, which a non-zero-exit trigger does not
# catch and which the no-body-inspection rule would otherwise write to
# stdout as file content -- the one failure mode where the caller cannot
# tell anything went wrong, since exit 0 plus plausible-looking bytes is
# indistinguishable from success. The probe's own response carries base64
# content that is downloaded and discarded; that waste is accepted because
# it is paid only on the rare, already-slow fallback path.
#
# The probe is also a default-media-type contents call, so it cannot
# inline a blob above roughly one megabyte -- such a file fails at the
# probe even though the fetch behind it could have read it. This is parity
# with what the skill documents today, not a regression; the raw path has
# no such ceiling.
PROBE_ENDPOINT="repos/$REPO/contents/$path"
PROBE_JQ='if type=="array" then "dir" else .type end'

gh api "$PROBE_ENDPOINT" --jq "$PROBE_JQ" >"$BODY_FILE" 2>"$STDERR_FILE"
probe_status=$?
if [ "$probe_status" -ne 0 ]; then
    diagnose "$PROBE_ENDPOINT" "$probe_status"
fi

probe_type="$(cat "$BODY_FILE")"
if [ "$probe_type" != "file" ]; then
    if [ "$probe_type" = "dir" ]; then
        die "github-read: repos/$REPO — '$path' is a directory, not a file; use github-tree.sh --children to list its entries"
    else
        die "github-read: repos/$REPO — '$path' is a $probe_type, not a file; use github-tree.sh --children to list its entries"
    fi
fi

# Only reached when the probe answered "file": the second call fetches the
# same contents endpoint with the raw media type, whose response body is
# the file content itself -- not the base64-plus-decode form, which
# inflates the transferred payload by roughly a third and adds a parse and
# a decode step for the same bytes over the same authenticated path.
gh api "$PROBE_ENDPOINT" -H "Accept: application/vnd.github.raw" >"$BODY_FILE" 2>"$STDERR_FILE"
content_status=$?
if [ "$content_status" -ne 0 ]; then
    # Nothing is ever written to stdout on this failure path, even though
    # the error body was written into $BODY_FILE the success path below
    # would have emitted.
    diagnose "$PROBE_ENDPOINT" "$content_status"
fi

cat "$BODY_FILE"
exit 0
