//go:build integration

// redditrss_integration_test.go drives the real unauthenticated Reddit .rss
// endpoint. It requires network access but explicitly **no** credentials, so
// it is excluded from the fast unit run and must be invoked explicitly with
// `go test -tags integration ./...`. It is a separate file from
// reddit_integration_test.go, whose own file doc comment scopes it to the
// OAuth API and whose skip-without-credentials contract is the opposite of
// this test's.

package main

import (
	"context"
	"strings"
	"testing"
)

// TestRedditRSSTier_Integration proves the RSS tier's two real requests --
// a subreddit feed fetch to discover a live thread, then a thread fetch
// through redditAdapter.Fetch -- both go through the process-wide limiter
// and actually work against Reddit's real .rss endpoint, which no offline
// test can do.
//
// Step 1 discovers the thread URL from a live subreddit feed rather than
// using a hard-coded thread id: a hard-coded thread rots -- the one
// reddit_integration_test.go uses today already returns a 404 from .rss --
// so a copy of that convention would ship already broken. Step 1 cannot go
// through Fetch itself, whose output is rendered markdown with no
// machine-readable entry link; fetchRedditRSSFeed is called directly to
// read the first entry's <link href>.
//
// t.Setenv(redditClientIDEnv/redditClientSecretEnv, "") and
// redditTokens.reset() force tier 1 into its no-request skip branch before
// step 2, since Fetch runs OAuth as tier 1 whenever both credential
// variables are set -- on a credentialed machine the assertions would
// otherwise pass without any of this task's code executing. "Does not
// require credentials" is not the same as "credentials are absent", and
// only the second guarantees the RSS tier produced the output.
//
// f.browser is replaced with a function calling t.Fatal, exactly as
// TestRedditOAuthThread_Integration does, so the never-falls-through-to-
// browser guarantee is enforced here too. stubRedditRSSLimiter is
// deliberately not called in this file -- the real limiter and the real
// wait are what this test exists to exercise.
func TestRedditRSSTier_Integration(t *testing.T) {
	f := newFetcher()
	f.browser = func(ctx context.Context, url string) (string, bool) {
		t.Fatal("browser fallback must never be called for a Reddit URL")
		return "", false
	}

	feed, err := fetchRedditRSSFeed(context.Background(), f, "https://www.reddit.com/r/golang/")
	if err != nil {
		t.Fatalf("fetchRedditRSSFeed() error = %v; want nil", err)
	}
	if len(feed.Entries) == 0 {
		t.Fatal("fetchRedditRSSFeed() returned a feed with no entries; want at least one to discover a thread URL from")
	}
	discoveredURL := feed.Entries[0].Link.Href
	if discoveredURL == "" {
		t.Fatal("fetchRedditRSSFeed() first entry has no link href; want a discoverable thread URL")
	}

	t.Setenv(redditClientIDEnv, "")
	t.Setenv(redditClientSecretEnv, "")
	redditTokens.reset()

	out, handled := (redditAdapter{}).Fetch(context.Background(), f, discoveredURL)
	if !handled {
		t.Fatalf("redditAdapter{}.Fetch(%q) handled = false; want true", discoveredURL)
	}
	if strings.HasPrefix(out, "# Error fetching ") {
		t.Fatalf("redditAdapter{}.Fetch(%q) returned an error result:\n%s", discoveredURL, out)
	}
	if _, blocked := looksLikeBlockPage(out); blocked {
		t.Fatalf("redditAdapter{}.Fetch(%q) returned a block/wall page:\n%s", discoveredURL, out)
	}
	if !strings.Contains(out, "Source: "+discoveredURL) {
		t.Fatalf("redditAdapter{}.Fetch(%q) output missing \"Source: %s\" line:\n%s", discoveredURL, discoveredURL, out)
	}
}
