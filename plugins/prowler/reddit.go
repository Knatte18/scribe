// reddit.go implements the Reddit site adapter as two tiers, tried in order and never falling
// through to the generic headless-browser cascade: the authenticated OAuth API when credentials are
// configured, then an unauthenticated Reddit .rss feed fetch (fetchRedditRSS, in redditrss.go), and
// finally a markdown error naming why each attempted tier failed.

package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// redditHostPattern matches Reddit URLs across its three host forms
// (bare, www, and old.reddit.com).
var redditHostPattern = regexp.MustCompile(`^https?://(www\.|old\.)?reddit\.com`)

// maxTopComments bounds top-level comments included when formatting
// threads, and (in redditoauth.go's formatRedditThread) also bounds the
// replies rendered under each top-level comment. Used by the Hacker News
// adapter and the Reddit OAuth adapter.
const maxTopComments = 20

// redditAdapter is the siteAdapter for Reddit: it tries the authenticated
// OAuth API, then an unauthenticated Reddit .rss feed fetch, and always
// reports handled=true -- it never falls through to the generic
// headless-browser cascade, since a second headless request against a
// solvable-looking Reddit challenge has been measured to escalate it into a
// hard IP-level block rather than recover it.
type redditAdapter struct{}

// Matches reports whether url points at Reddit, across its bare, www, and old.reddit.com host
// forms.
func (redditAdapter) Matches(url string) bool {
	return redditHostPattern.MatchString(url)
}

// Fetch runs url through Reddit's two fetch tiers in order and always
// reports handled=true, so a Reddit URL never falls through to fetchPage's
// generic static-fetch/Readability/browser cascade -- in particular, f.browser
// is never called from this method, directly or indirectly.
//
// Tier 1 uses the authenticated OAuth API (fetchRedditOAuthThread) when
// redditCredentials reports no missing environment variables; when
// credentials are absent, this tier is skipped without issuing any request.
// Tier 2 falls back to an unauthenticated Reddit .rss feed fetch
// (fetchRedditRSS), which needs no credentials and no app registration and
// is paced against Reddit's roughly one-request-per-60-seconds per-IP
// window. When both tiers fail, Fetch returns an errorResult-formatted
// markdown error listing each attempted tier and its failure reason.
func (redditAdapter) Fetch(ctx context.Context, f fetcher, url string) (string, bool) {
	var attempts []string

	// redditCredentials is re-consulted here (rather than only inside
	// fetchRedditOAuthThread) so a missing-credentials skip can be recorded
	// without issuing any request at all.
	_, _, missing := redditCredentials()
	if len(missing) == 0 {
		out, err := fetchRedditOAuthThread(ctx, f, url)
		if err == nil {
			return out, true
		}
		attempts = append(attempts, fmt.Sprintf("- Tier 1 (Reddit OAuth API): %s", err))
	} else {
		attempts = append(attempts, fmt.Sprintf("- Tier 1 (Reddit OAuth API): skipped, missing environment variables: %s", strings.Join(missing, ", ")))
	}

	out, err := fetchRedditRSS(ctx, f, url)
	if err == nil {
		return out, true
	}
	attempts = append(attempts, fmt.Sprintf("- Tier 2 (Reddit RSS): %s", err))

	return errorResult(url, strings.Join(attempts, "\n")), true
}
