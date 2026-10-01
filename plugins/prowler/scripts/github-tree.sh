#!/usr/bin/env bash
# github-tree.sh lists a GitHub repository's file paths in one deterministic
# invocation, replacing an N-call LLM-driven tree walk (resolve branch, list
# recursive tree, check truncation, fall back to per-directory listing) with
# a single script call the model no longer has to compose turn by turn.
#
# Stdout carries exactly the path list, one repo-relative file path per
# line, and nothing else -- every diagnostic goes to stderr. The listing is
# buffered in memory and printed only once the whole walk has succeeded, so
# a failure partway through never leaves a partial prefix on stdout for a
# caller to mistake for a complete (if short) listing.
#
# There are deliberately no retries and no backoff anywhere in this script:
# every non-2xx response from `gh api` aborts the run immediately with one
# stderr line, and it is the caller's job to decide whether to re-invoke.
# This is not an oversight to "fix" by adding a retry loop -- a transient
# failure and a permanent one (bad path, no auth, repo renamed) look
# identical from here, and only the caller has enough context to tell them
# apart.
#
# Unlike run.sh, this script reads no file inside the plugin -- it takes an
# owner/repo and an optional path and calls `gh`, nothing else -- so it does
# not self-locate a SCRIPT_DIR/PLUGIN_ROOT. What it does copy from run.sh is
# the strict stdout discipline and the `command -v` prerequisite check.
#
# Flags are recognised only ahead of the first positional argument: once one
# positional has been collected, a later token beginning with two dashes is
# a usage error rather than a silently-ignored flag or a reinterpreted path.
# A `--` terminator ends flag recognition early and makes every remaining
# token -- including one beginning with two dashes -- a positional, which is
# the only way to pass such a path to this script.
#
# An entry-count guard aborts the walk, incrementally, once the buffered
# listing exceeds a ceiling that defaults to 1000 entries and is overridable
# with `--max-entries N`; `--max-entries 0` disables the ceiling entirely.
# The check runs on every append, not once at the end, so a listing that
# would exceed the ceiling never burns the rest of its `gh` call budget
# just to arrive at a rejection already determined earlier in the walk.
# `--max-entries` is always read as base-10, leading zeros included (e.g.
# `010` means ten, not eight), so it never falls into bash's octal-literal
# arithmetic parsing.
#
# `--children` lists one path's direct children without recursing: a
# directory entry is marked with a single trailing slash, a file entry
# carries no marker, and a trailing slash cannot collide with a file name
# because a blob path never ends in one.
set -u

# die prints one message to stderr and exits 1. It is not used for the
# usage error, which is the one case that exits 2 instead.
die() {
    echo "$1" >&2
    exit 1
}

# --- Prerequisite and argument handling, in this exact order, so that a --
# --- rejection never reaches the network. --------------------------------

if ! command -v gh >/dev/null 2>&1; then
    die "github-tree: gh not found on PATH — install the GitHub CLI and authenticate it (gh auth login)"
fi

# usage prints the usage line to stderr and exits 2. It is deliberately not
# routed through die, which exits 1 -- exit 2 is reserved for malformed
# invocations, exit 1 for every operational failure.
usage() {
    echo "github-tree: usage: github-tree.sh [--children] [--max-entries N] <owner/repo> [path]" >&2
    exit 2
}

CHILDREN=0
MAX_ENTRIES=1000

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
    --children)
        [ "${#args[@]}" -eq 0 ] || usage
        CHILDREN=1
        shift
        ;;
    --max-entries)
        [ "${#args[@]}" -eq 0 ] || usage
        [ "$#" -ge 2 ] || usage
        MAX_ENTRIES="$2"
        shift 2
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

case "$MAX_ENTRIES" in
'' | *[!0-9]*) usage ;;
esac

# Force base-10 interpretation and strip any leading zeros: bash treats a
# leading-zero numeric literal (e.g. 010) as octal in arithmetic context,
# which would silently misapply the ceiling (010 -> 8) or crash outright on
# an invalid octal digit (018 -> "value too great for base"). Normalizing
# here, once, keeps both the "0 means unlimited" string comparison in emit
# and the arithmetic comparison against ${#output[@]} working against the
# same canonical decimal value.
MAX_ENTRIES=$((10#$MAX_ENTRIES))

[ "${#args[@]}" -ge 1 ] && [ "${#args[@]}" -le 2 ] || usage

REPO="${args[0]}"
RAW_PATH="${args[1]:-}"

if ! [[ "$REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    die "github-tree: '$REPO' is not a valid <owner>/<repo> reference"
fi

# Normalize the path: strip every leading and trailing '/'. A result that
# is empty after stripping means "whole repo".
path="$RAW_PATH"
while [[ "$path" == /* ]]; do path="${path#/}"; done
while [[ "$path" == */ ]]; do path="${path%/}"; done

if [ -n "$path" ]; then
    # Validate by deleting every accepted character from a copy of the
    # path and checking what remains, rather than testing the whole
    # string against `[[ =~ ^[A-Za-z0-9._/-]+$ ]]` or indexing it byte by
    # byte to find "the first offender".
    #
    # Regex bracket ranges like A-Z are collation-ordered, not codepoint-
    # ordered: under a UTF-8 locale (this box's default), glibc's regex
    # engine treats accented letters such as 'ï' as falling inside that
    # collation range, so `[[ "naïve" =~ ^[A-Za-z0-9._/-]+$ ]]` wrongly
    # matches -- a real, reproduced failure mode, not a hypothetical one.
    # Bash's glob-pattern matching (which parameter-expansion substitution
    # uses) does not have this problem, so it is used for both detection
    # and the offending-character report.
    #
    # Byte indexing has a separate, independent problem: it is byte-
    # oriented under a C/POSIX locale, which minimal shells and CI images
    # routinely run with and which this script deliberately does not pin,
    # so indexing would slice a multi-byte UTF-8 character into single
    # invalid bytes and report one of them as "the character". The glob
    # substitution below is immune to that too: the accepted set is pure
    # ASCII, so a UTF-8 continuation byte is never in it and no multi-byte
    # sequence can be split -- the remainder always holds each offending
    # character whole. Reporting the full remaining set rather than just
    # the first offender is the same choice made for the same reason:
    # there is no first-offender extraction that is byte-safe without
    # re-introducing the indexing problem.
    offending="${path//[A-Za-z0-9._\/-]/}"
    if [ -n "$offending" ]; then
        die "github-tree: path '$path' contains unsupported character(s) '$offending' — only [A-Za-z0-9._/-] is accepted"
    fi
fi

# Deliberately no other rewriting: internal '//', '.', and '..' segments
# pass straight through to the API and surface as a loud error there rather
# than being silently reinterpreted here.

if [ -z "$path" ]; then
    BASE_REF="HEAD"
    PREFIX=""
else
    BASE_REF="HEAD:$path"
    PREFIX="$path/"
fi

# One jq expression, applied to both the recursive and non-recursive tree
# endpoints, yields the truncation flag and the entry list from a single
# `gh api` call -- the direct fix for the duplicate call the old walk made
# against the recursive endpoint just to re-check truncation.
JQ_EXPR='"#trunc\t" + (.truncated|tostring), (.tree[] | if (.path|test("[\t\n]")) then "#badpath\t" + (.path|@json) else .type + "\t" + .sha + "\t" + .path end)'

# fetch <endpoint> <kind> runs exactly `gh api "<endpoint>" --jq "<expr>"`
# -- four arguments, no other flags, ever -- and on success leaves its
# result in the globals FETCH_TRUNCATED (the boolean string "true"/"false")
# and FETCH_ENTRIES (an array of "type\tsha\tpath" lines, badpath sentinels
# already handled here). <kind> is "root" for the two fetches that address
# the walk's seed item (the initial recursive fetch of BASE_REF and, if
# that is truncated, the non-recursive re-fetch of the same BASE_REF) and
# "child" for every other fetch, which addresses a subtree by the sha its
# parent listing gave. Only a "root" fetch can be the one carrying the
# caller's path, so only there is a 404 attributable to that path rather
# than to a subtree that vanished mid-walk.
fetch() {
    local endpoint="$1" kind="$2" captured status http body

    captured="$(gh api "$endpoint" --jq "$JQ_EXPR" 2>/dev/null)"
    status=$?

    if [ "$status" -ne 0 ]; then
        http=""
        if [[ "$captured" =~ \"status\"[[:space:]]*:[[:space:]]*\"?([0-9]{3})\"? ]]; then
            http="${BASH_REMATCH[1]}"
        fi
        # Collapse the body to one physical line before embedding it.
        # GitHub's error bodies are compact JSON in practice, but nothing
        # guarantees it, and the failure contract promises the caller
        # exactly one stderr line -- a promise kept literally, not by
        # assumption.
        body="${captured//$'\n'/ }"

        if [ "$http" = "401" ]; then
            die "github-tree: repos/$REPO — not authenticated (HTTP 401); run 'gh auth login'"
        elif [ "$http" = "403" ]; then
            die "github-tree: repos/$REPO — rate limited or access denied (HTTP 403)"
        elif { [ "$http" = "404" ] || [ "$http" = "409" ]; } && [ "$kind" = "root" ] && [ -z "$path" ]; then
            die "github-tree: repos/$REPO — not found, may not be accessible with this token, or has no commits yet (HTTP $http)"
        elif [ "$http" = "404" ] && [ "$kind" = "root" ] && [ -n "$path" ]; then
            die "github-tree: repos/$REPO — path '$path' not found (HTTP 404)"
        elif [ "$http" = "422" ]; then
            die "github-tree: repos/$REPO — path '$path' is not a directory (HTTP 422)"
        else
            die "github-tree: gh api $endpoint failed (exit $status): $body"
        fi
    fi

    FETCH_TRUNCATED=""
    FETCH_ENTRIES=()
    local line seen_header=0

    # A here-string, never a pipeline, so the loop body runs in the main
    # shell and a failure inside it (the badpath abort) can actually abort
    # the run instead of only exiting a subshell.
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        if [ "$seen_header" -eq 0 ]; then
            FETCH_TRUNCATED="${line#*$'\t'}"
            seen_header=1
            continue
        fi
        case "$line" in
        $'#badpath\t'*)
            local escaped="${line#$'#badpath\t'}"
            die "github-tree: repos/$REPO — refusing to list: a path contains a tab or newline ($escaped), which the one-path-per-line output cannot represent"
            ;;
        esac
        FETCH_ENTRIES+=("$line")
    done <<<"$captured"
}

output=()

# emit appends one path to output and enforces the entry-count guard on the
# crossing append, not once at the end -- an abort here is what keeps a
# truncated-fallback walk from burning its whole multi-call budget to
# produce a rejection already determined by the first few hundred entries.
# Checking strictly greater than after the append is what lets a listing of
# exactly MAX_ENTRIES entries succeed while MAX_ENTRIES plus one aborts.
# MAX_ENTRIES=0 means unlimited. The abort message is mode-aware: it never
# suggests --children back to a caller who is already using it.
emit() {
    output+=("$1")
    if [ "$MAX_ENTRIES" != "0" ] && [ "${#output[@]}" -gt "$MAX_ENTRIES" ]; then
        if [ "$CHILDREN" -eq 1 ]; then
            die "github-tree: repos/$REPO — listing exceeds $MAX_ENTRIES entries; scope to a subdirectory or raise --max-entries"
        else
            die "github-tree: repos/$REPO — listing exceeds $MAX_ENTRIES entries; scope to a subdirectory, use --children, or raise --max-entries"
        fi
    fi
}

if [ "$CHILDREN" -eq 1 ]; then
    # --children lists exactly one path's direct children and never
    # recurses: one fetch against the non-recursive endpoint (no
    # ?recursive=1 suffix), reusing fetch/BASE_REF/PREFIX/JQ_EXPR unchanged
    # so a --children run makes exactly one gh call. The non-recursive
    # truncation abort is restated here rather than inherited, because it
    # lives inside the recursive walk's own else branch below, which this
    # arm never enters.
    fetch "repos/$REPO/git/trees/$BASE_REF" "root"
    if [ "$FETCH_TRUNCATED" = "true" ]; then
        die "github-tree: repos/$REPO — the non-recursive listing of '$BASE_REF' is itself truncated; this repository has a directory too large for the GitHub tree API"
    fi
    # The [ -gt 0 ] guard keeps an empty directory a genuine no-op under
    # set -u rather than an unbound-variable error.
    if [ "${#FETCH_ENTRIES[@]}" -gt 0 ]; then
        for entry in "${FETCH_ENTRIES[@]}"; do
            etype="${entry%%$'\t'*}"
            erest="${entry#*$'\t'}"
            epath="${erest#*$'\t'}"
            case "$etype" in
            blob)
                emit "$PREFIX$epath"
                ;;
            tree)
                # A single trailing slash marks a directory; a blob path
                # never ends in one, so the marker cannot collide with a
                # file name. This is the one place the recursive modes'
                # blob-paths-only output contract does not apply.
                emit "$PREFIX$epath/"
                ;;
            *)
                # "commit" entries (submodules) are skipped silently, same
                # reason as the recursive walk below.
                ;;
            esac
        done
    fi
else
    # The walk itself: one explicit FIFO queue in the main shell, never a
    # recursive shell function and never a LIFO stack, so output order is
    # fixed to the order work items were appended -- the root's own blobs
    # first, then each subtree's blobs in the order its parent listing gave
    # them, at every depth.
    #
    # Each queue item is a tab-separated 4-tuple: mode ("rec" or "nonrec"),
    # a ref, a path prefix, and a kind ("root" or "child", see fetch()
    # above).
    queue=("rec"$'\t'"$BASE_REF"$'\t'"$PREFIX"$'\t'"root")
    head=0

    while [ "$head" -lt "${#queue[@]}" ]; do
        item="${queue[$head]}"
        head=$((head + 1))

        mode="${item%%$'\t'*}"
        rest="${item#*$'\t'}"
        ref="${rest%%$'\t'*}"
        rest="${rest#*$'\t'}"
        prefix="${rest%%$'\t'*}"
        kind="${rest#*$'\t'}"

        if [ "$mode" = "rec" ]; then
            fetch "repos/$REPO/git/trees/$ref?recursive=1" "$kind"
            if [ "$FETCH_TRUNCATED" = "true" ]; then
                # Truncated: re-fetch the same ref non-recursively instead
                # of trusting this partial listing.
                queue+=("nonrec"$'\t'"$ref"$'\t'"$prefix"$'\t'"$kind")
                continue
            fi
            for entry in "${FETCH_ENTRIES[@]}"; do
                etype="${entry%%$'\t'*}"
                erest="${entry#*$'\t'}"
                epath="${erest#*$'\t'}"
                if [ "$etype" = "blob" ]; then
                    emit "$prefix$epath"
                fi
                # "tree" entries within an untruncated recursive listing
                # need no further queueing -- the recursive listing
                # already descended into them. "commit" entries
                # (submodules) are skipped silently: a submodule path is
                # not readable through this repository's contents API.
            done
        else
            fetch "repos/$REPO/git/trees/$ref" "$kind"
            if [ "$FETCH_TRUNCATED" = "true" ]; then
                die "github-tree: repos/$REPO — the non-recursive listing of '$ref' is itself truncated; this repository has a directory too large for the GitHub tree API"
            fi
            for entry in "${FETCH_ENTRIES[@]}"; do
                etype="${entry%%$'\t'*}"
                erest="${entry#*$'\t'}"
                esha="${erest%%$'\t'*}"
                epath="${erest#*$'\t'}"
                case "$etype" in
                blob)
                    emit "$prefix$epath"
                    ;;
                tree)
                    queue+=("rec"$'\t'"$esha"$'\t'"$prefix$epath/"$'\t'"child")
                    ;;
                *)
                    # "commit" entries (submodules) are skipped silently,
                    # same reason as above.
                    ;;
                esac
            done
        fi
    done
fi

# Nothing is written to stdout until the queue is exhausted with no error.
# Zero blobs is a success (exit 0, empty stdout) -- the guard below just
# keeps the emission a genuine no-op under `set -u` in that case.
if [ "${#output[@]}" -gt 0 ]; then
    printf '%s\n' "${output[@]}"
fi

exit 0
