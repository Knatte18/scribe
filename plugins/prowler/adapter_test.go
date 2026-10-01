// adapter_test.go exercises fetchPage's site-adapter routing loop directly, independent of the
// concrete Reddit/Hacker News adapters, using a local configurable stub implementation of
// siteAdapter.

package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// stubAdapter is a configurable siteAdapter for testing: matches controls
// Matches' return value, fetchCalled records whether Fetch was invoked.
type stubAdapter struct {
	matches     bool
	fetchCalled *bool
	out         string
	handled     bool
}

// Matches implements siteAdapter by returning the configured matches value.
func (s stubAdapter) Matches(string) bool { return s.matches }

// Fetch implements siteAdapter by recording the call and returning the configured canned result.
func (s stubAdapter) Fetch(context.Context, fetcher, string) (string, bool) {
	if s.fetchCalled != nil {
		*s.fetchCalled = true
	}
	return s.out, s.handled
}

func TestFetchPage_AdapterRouting(t *testing.T) {
	const url = "https://example.com/whatever"

	t.Run("matched_handled_adapter_short_circuits_transport", func(t *testing.T) {
		f := fetcher{
			do: func(*http.Request) (*http.Response, error) {
				t.Fatal("transport should not be invoked when an adapter handles the fetch")
				return nil, nil
			},
			adapters: []siteAdapter{stubAdapter{matches: true, out: "# Adapter Output", handled: true}},
		}

		got := fetchPage(context.Background(), f, url)
		if got != "# Adapter Output" {
			t.Errorf("fetchPage() = %q; want the adapter's output", got)
		}
	})

	t.Run("matched_unhandled_adapter_falls_through_to_generic_cascade", func(t *testing.T) {
		f := stubResponses(t, map[string]*http.Response{
			url: htmlResponse(readableArticleHTML),
		}, func(context.Context, string) (string, bool) {
			t.Fatal("browser fallback should not be invoked; Readability should succeed")
			return "", false
		})
		f.adapters = []siteAdapter{stubAdapter{matches: true, handled: false}}

		got := fetchPage(context.Background(), f, url)
		if !strings.Contains(got, "genuinely readable article content") {
			t.Errorf("fetchPage() = %q; want it to fall through to the generic cascade", got)
		}
	})

	t.Run("only_first_matching_adapter_is_consulted", func(t *testing.T) {
		var secondCalled bool
		f := fetcher{
			adapters: []siteAdapter{
				stubAdapter{matches: true, out: "# First Adapter", handled: true},
				stubAdapter{matches: true, fetchCalled: &secondCalled, out: "# Second Adapter", handled: true},
			},
		}

		got := fetchPage(context.Background(), f, url)
		if got != "# First Adapter" {
			t.Errorf("fetchPage() = %q; want the first adapter's output", got)
		}
		if secondCalled {
			t.Error("fetchPage() called the second matching adapter's Fetch; want it never consulted")
		}
	})

	t.Run("non_matching_adapter_is_skipped", func(t *testing.T) {
		var fetchCalled bool
		f := stubResponses(t, map[string]*http.Response{
			url: htmlResponse(readableArticleHTML),
		}, func(context.Context, string) (string, bool) {
			t.Fatal("browser fallback should not be invoked; Readability should succeed")
			return "", false
		})
		f.adapters = []siteAdapter{stubAdapter{matches: false, fetchCalled: &fetchCalled, out: "# Should Not Be Used", handled: true}}

		got := fetchPage(context.Background(), f, url)
		if fetchCalled {
			t.Error("fetchPage() called Fetch on a non-matching adapter; want it skipped")
		}
		if !strings.Contains(got, "genuinely readable article content") {
			t.Errorf("fetchPage() = %q; want it to take the generic cascade", got)
		}
	})
}
