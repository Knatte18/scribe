#!/usr/bin/env bash
# github-code-search.sh runs one GitHub code search query across one or
# more repositories in a single invocation, replacing an N-call
# LLM-driven "search each repo, one at a time, retyping the query each
# time" loop with a single script call the model no longer has to
# compose turn by turn.
#
# Stdout carries exactly one tab-separated record per matching file --
# <owner>/<repo>\t<path>\t<snippet> -- and nothing else; every
# diagnostic goes to stderr. Every record across every repo is buffered
# in memory and printed only once the whole sweep has succeeded, so a
# failure partway through never leaves a partial prefix on stdout for a
# caller to mistake for a complete (if short) result set. With a
# multi-repo sweep the risk is worse than for a single-repo listing: a
# prefix covering repos 1-3 of 8 looks exactly like a sweep where repos
# 4-8 had no hits.
#
# There are deliberately no retries and no backoff anywhere in this
# script: every non-2xx response from `gh api` aborts the run
# immediately with one stderr line, and it is the caller's job to decide
# whether to re-invoke. This is not an oversight to "fix" by adding a
# retry loop -- a transient 403 (secondary rate limit) and a permanent
# one (token lacks access) are indistinguishable from inside the script,
# and only the caller has enough context to tell them apart.
#
# A hit is always emitted with three fields, even when GitHub returned
# no text_matches for it: there is no flag and no second output mode,
# because dropping the record would silently under-report matches and a
# fixed field count means a caller can split on tabs without a special
# case.
#
# Repos are capped at ten distinct refs per invocation, deduplicated
# before the cap is applied so the cap is exact -- ten distinct refs is
# always ten calls, never eleven. The cap exists because GitHub's
# code_search API bucket allows only ten requests per minute; a larger
# sweep burns that bucket for every other caller sharing the same token,
# not just this one.
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
    die "github-code-search: gh not found on PATH — install the GitHub CLI and authenticate it (gh auth login)"
fi

if [ "$#" -lt 2 ]; then
    echo "github-code-search: usage: github-code-search.sh <query> <owner/repo> [<owner/repo>...]" >&2
    exit 2
fi

QUERY="$1"
shift

# Arity alone does not catch an empty or whitespace-only query, and live
# behaviour makes it dangerous: a query carrying only a repo qualifier
# still returns hits, so an empty query silently degrades into "the
# first 100 indexed files in this repo" -- a tree listing wearing a
# search's output shape, burning a request from the scarcest bucket in
# the plugin to produce something github-tree.sh already provides for
# free.
trimmed_query="${QUERY//[[:space:]]/}"
if [ -z "$trimmed_query" ]; then
    die "github-code-search: the query argument must not be empty or whitespace-only"
fi

# The script appends its own repo: qualifier last, and the last one
# wins, so a caller-supplied repo: would be silently discarded with no
# diagnostic. This is a plain substring test on the whole query, not a
# qualifier-position test, so it also rejects a search for the literal
# token as file content -- an accepted, documented limitation rather
# than a bug to engineer around, since distinguishing the two means
# reimplementing GitHub's query tokenizer in bash.
case "$QUERY" in
*repo:*)
    die "github-code-search: the query must not contain 'repo:' — pass repo refs as positional arguments instead; if you need an explicit qualifier, use a raw 'gh api -X GET search/code -f q=...' call"
    ;;
esac

for ref in "$@"; do
    if ! [[ "$ref" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
        die "github-code-search: '$ref' is not a valid <owner>/<repo> reference"
    fi
done

# Deduplicate, keeping first-occurrence order, comparing on the
# lowercased ref rather than the raw string, and keeping the caller's
# original spelling of the surviving first occurrence as the ref
# forwarded into the qualifier. GitHub refs are case-insensitive, so an
# exact-string comparison would let two spellings of one repo both
# survive, burn two of the ten requests on it, and emit its records
# twice under one identical name.
declare -A seen_refs
REFS=()
for ref in "$@"; do
    lower="${ref,,}"
    if [ -z "${seen_refs[$lower]:-}" ]; then
        seen_refs[$lower]=1
        REFS+=("$ref")
    fi
done

# Deduplication before the cap is what makes the cap exact -- ten
# distinct refs is ten calls, never eleven.
if [ "${#REFS[@]}" -gt 10 ]; then
    die "github-code-search: too many distinct repos (${#REFS[@]}) — at most 10 distinct <owner>/<repo> refs per invocation; the code_search API bucket allows only 10 requests per minute"
fi

PREFLIGHT_JQ_EXPR='.full_name'

# One jq expression, applied identically by the real gh's embedded gojq
# at run time and by system jq inside the selftest harness, emits one
# header line (the two response-level fields the records cannot carry,
# plus the returned item count) followed by one line per item. The
# #badpath sentinel carries over from github-tree.sh's own jq expression
# and is extended to cover the repository name field as well as the
# path field, because either one containing a tab or newline would
# break the one-record-per-line output. A property of "path" rather than
# "content" on a text_match means the fragment is the matched path
# rather than file content -- worth noting here since it is not branched
# on.
SEARCH_JQ_EXPR='"#meta\t" + (.total_count|tostring) + "\t" + (.incomplete_results|tostring) + "\t" + ((.items|length)|tostring), (.items[] | if ((.repository.full_name | test("[\t\n\r]")) or (.path | test("[\t\n\r]"))) then "#badpath\t" + ((.repository.full_name + "/" + .path) | @json) else .repository.full_name + "\t" + .path + "\t" + ((.text_matches // []) | (if length > 0 then (.[0].fragment // "") else "" end) | gsub("[\t\r\n]+"; " ") | .[0:200]) end)'

# extract_http_status recovers the HTTP status embedded in a `gh api`
# JSON error body, using the same regex github-tree.sh's own fetch()
# uses, and leaves it in the global HTTP_STATUS (empty when none was
# found).
extract_http_status() {
    local body="$1"
    HTTP_STATUS=""
    if [[ "$body" =~ \"status\"[[:space:]]*:[[:space:]]*\"?([0-9]{3})\"? ]]; then
        HTTP_STATUS="${BASH_REMATCH[1]}"
    fi
}

# --- Preflight loop: run to completion before any search call. ---------
#
# A nonexistent repo's search returns HTTP 200 with a zero total_count,
# byte-identical to a real repo with no matches -- without this loop a
# typo'd or private repo silently reads as a confident negative answer,
# the worst available failure mode in the reconnaissance use case this
# script serves. It costs nothing scarce: it hits the 5000-per-hour core
# bucket, not the ten-per-minute search bucket. Running the whole loop to
# completion before any search call is deliberate, not incidental -- a
# per-repo interleaving would spend search requests on early repos
# before discovering that a later ref is unusable.
for ref in "${REFS[@]}"; do
    captured="$(gh api "repos/$ref" --jq "$PREFLIGHT_JQ_EXPR" 2>/dev/null)"
    status=$?

    if [ "$status" -ne 0 ]; then
        extract_http_status "$captured"
        # Collapse the body to one physical line before embedding it --
        # the one-stderr-line promise is kept literally, not by
        # assumption.
        body="${captured//$'\n'/ }"

        if [ "$HTTP_STATUS" = "401" ]; then
            die "github-code-search: repos/$ref — not authenticated (HTTP 401); run 'gh auth login'"
        elif [ "$HTTP_STATUS" = "403" ]; then
            die "github-code-search: repos/$ref — access denied or rate limited (HTTP 403)"
        elif [ "$HTTP_STATUS" = "404" ]; then
            die "github-code-search: repos/$ref — not found or not accessible with this token (HTTP 404)"
        else
            die "github-code-search: gh api repos/$ref failed (exit $status): $body"
        fi
    fi
done

# --- Search loop: one call per deduplicated ref, in order. -------------
OUTPUT=()
for ref in "${REFS[@]}"; do
    # -X GET is mandatory and is the single subtlest thing in this
    # script: `gh api` defaults to POST as soon as any -f parameter is
    # present, which would send the query in a request body the search
    # endpoint ignores. This "works" against a stub that does not check
    # it and fails only live.
    captured="$(gh api -X GET "search/code" -f "q=$QUERY repo:$ref" -f "per_page=100" -H "Accept: application/vnd.github.text-match+json" --jq "$SEARCH_JQ_EXPR" 2>/dev/null)"
    status=$?

    if [ "$status" -ne 0 ]; then
        extract_http_status "$captured"
        body="${captured//$'\n'/ }"
        if [ -n "$HTTP_STATUS" ]; then
            die "github-code-search: search/code repo:$ref — request failed (HTTP $HTTP_STATUS)"
        else
            die "github-code-search: gh api search/code repo:$ref failed (exit $status): $body"
        fi
    fi

    total=""
    incomplete=""
    item_count=""
    seen_header=0

    # A here-string, never a pipeline, so the loop body runs in the main
    # shell and a failure inside it (the badpath abort) can actually
    # abort the run instead of only exiting a subshell.
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        if [ "$seen_header" -eq 0 ]; then
            IFS=$'\t' read -r _tag total incomplete item_count <<<"$line"
            seen_header=1
            # The incomplete-results flag means GitHub returned a
            # partial result set -- the search timed out or the query
            # was rejected in a way that still yields 200 -- and
            # treating that as "no matches" is the same silent-partial
            # failure github-tree.sh refuses for truncated trees.
            if [ "$incomplete" = "true" ]; then
                die "github-code-search: search/code repo:$ref — GitHub returned a partial result set (incomplete_results=true); re-run once results stabilize"
            fi
            continue
        fi
        case "$line" in
        $'#badpath\t'*)
            escaped="${line#$'#badpath\t'}"
            die "github-code-search: repos/$ref — refusing to emit a record for $escaped; the one-record-per-line output cannot represent an embedded tab or newline"
            ;;
        esac
        OUTPUT+=("$line")
    done <<<"$captured"

    # Exit status is unaffected by a capped total: the returned records
    # are all genuine and complete-as-far-as-they-go, so failing the run
    # would destroy usable output, while staying silent would make a
    # capped listing indistinguishable from a complete one. The note
    # goes to stderr specifically so it never pollutes the record stream
    # a caller greps.
    if [ -n "$total" ] && [ -n "$item_count" ] && [ "$total" -gt "$item_count" ]; then
        echo "github-code-search: repos/$ref — showing $item_count of $total matches (GitHub search results are capped); refine the query for full coverage" >&2
    fi
done

# Nothing is written to stdout until every repo has succeeded. A sweep
# with zero matches across every repo is a success: exit 0 with
# byte-empty stdout -- the guard below just keeps the emission a genuine
# no-op under `set -u` in that case.
if [ "${#OUTPUT[@]}" -gt 0 ]; then
    printf '%s\n' "${OUTPUT[@]}"
fi

exit 0
