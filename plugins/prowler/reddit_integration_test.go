//go:build integration

// reddit_integration_test.go drives the real Reddit OAuth API against a
// known-public live thread. It requires network access and real Reddit
// credentials (PROWLER_REDDIT_CLIENT_ID / PROWLER_REDDIT_CLIENT_SECRET), so
// it is excluded from the fast unit run and must be invoked explicitly with
// `go test -tags integration ./...`.

package main

import (
	"context"
	"strings"
	"testing"
)

// liveRedditThreadURL is one hard-coded public Reddit thread fetched
// exactly once by TestRedditOAuthThread_Integration -- no loop, no retry,
// no table of URLs -- because repeated, unpaced live Reddit requests
// degrade this IP's standing (see _mill/discussion.md). The RSS tier's two
// requests in redditrss_integration_test.go are consistent with that same
// intent despite being a second live-Reddit test in this package: they are
// correctly paced against Reddit's own reported rate-limit window by
// redditRSSLimiter, rather than being an unpaced repeat of this test's
// single request.
const liveRedditThreadURL = "https://www.reddit.com/r/announcements/comments/5e19z2/every_time_you_write_reddit_in_all_caps_you_are/"

// TestRedditOAuthThread_Integration proves the client_credentials grant
// actually works against oauth.reddit.com's endpoints, which no offline
// test can do. It skips when PROWLER_REDDIT_CLIENT_ID or
// PROWLER_REDDIT_CLIENT_SECRET is absent, naming the missing variables, and
// resets redditTokens first so the run cannot pass on a token cached by
// another test. The fetcher's browser field is replaced with a function
// that calls t.Fatal, so the test fails loudly rather than silently
// succeeding through the browser tier if redditAdapter's
// never-falls-through-to-browser guarantee ever regresses.
func TestRedditOAuthThread_Integration(t *testing.T) {
	_, _, missing := redditCredentials()
	if len(missing) > 0 {
		t.Skip("missing environment variables: " + strings.Join(missing, ", "))
	}

	redditTokens.reset()

	f := newFetcher()
	f.browser = func(ctx context.Context, url string) (string, bool) {
		t.Fatal("browser fallback must never be called for a Reddit URL")
		return "", false
	}

	out, handled := (redditAdapter{}).Fetch(context.Background(), f, liveRedditThreadURL)
	if !handled {
		t.Fatalf("redditAdapter{}.Fetch(%q) handled = false; want true", liveRedditThreadURL)
	}
	if strings.HasPrefix(out, "# Error fetching ") {
		t.Fatalf("redditAdapter{}.Fetch(%q) returned an error result:\n%s", liveRedditThreadURL, out)
	}
	if _, blocked := looksLikeBlockPage(out); blocked {
		t.Fatalf("redditAdapter{}.Fetch(%q) returned a block/wall page:\n%s", liveRedditThreadURL, out)
	}
	if !strings.Contains(out, "Source: "+liveRedditThreadURL) {
		t.Fatalf("redditAdapter{}.Fetch(%q) output missing \"Source: %s\" line:\n%s", liveRedditThreadURL, liveRedditThreadURL, out)
	}
}
