// fetcher.go defines the injection seam every fetch code path is written against, so the whole
// static-fetch/Reddit/browser cascade can be unit tested with stubbed transports instead of real
// network or Chrome calls.

package main

import (
	"context"
	"net/http"
)

// fetcher bundles the side-effecting operations the fetch cascade needs:
// issuing HTTP requests and driving a headless-browser fallback. do and
// browser must both be set before use -- neither has a nil-fallback, so an
// unset field is a wiring bug that must fail loudly rather than silently
// substitute another field's transport semantics.
type fetcher struct {
	// do performs the raw HTTP transport, following redirects.
	do func(*http.Request) (*http.Response, error)

	// browser drives a headless-Chrome fallback fetch and reports whether it
	// produced usable content (empty string with false means unavailable/failed).
	browser func(ctx context.Context, url string) (string, bool)

	// adapters are the site-specific fetch strategies fetchPage tries before
	// falling back to the generic cascade. Nil or empty slice is valid.
	adapters []siteAdapter
}
