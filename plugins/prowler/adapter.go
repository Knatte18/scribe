// adapter.go defines the pluggable site-adapter mechanism that fetchPage consults before falling
// back to the generic static-fetch/Readability/ browser cascade.
// Each adapter owns a higher-fidelity strategy for one site (e.g.
// Reddit's unauthenticated .rss feed, Hacker News' Algolia API) so site-specific quirks live in
// their own file rather than as special-cased branches inside fetch.go.

package main

import "context"

// siteAdapter is a higher-fidelity fetch strategy for one site. Matches
// reports whether url belongs to this adapter's site. Fetch attempts the
// adapter's strategy, reporting handled=false when it cannot produce usable
// content.
type siteAdapter interface {
	// Matches reports whether url belongs to this adapter's site.
	Matches(url string) bool

	// Fetch attempts this adapter's strategy via f. handled=false means the
	// strategy could not produce usable content.
	Fetch(ctx context.Context, f fetcher, url string) (out string, handled bool)
}

// defaultAdapters returns the production site-adapter set: Reddit first,
// then Hacker News.
func defaultAdapters() []siteAdapter {
	return []siteAdapter{redditAdapter{}, hackerNewsAdapter{}}
}
