// hackernews_test.go exercises Hacker News item-URL matching and hackerNewsAdapter.Fetch's
// formatting/failure branches via a stubbed fetcher.do keyed on the Algolia API URL.
// No network.

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHackerNewsAdapterMatches(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"item_https", "https://news.ycombinator.com/item?id=1", true},
		{"item_http", "http://news.ycombinator.com/item?id=42", true},
		{"item_www", "https://www.news.ycombinator.com/item?id=7", true},
		{"newest_page", "https://news.ycombinator.com/newest", false},
		{"front_page", "https://news.ycombinator.com/", false},
		{"missing_id", "https://news.ycombinator.com/item", false},
		{"non_numeric_id", "https://news.ycombinator.com/item?id=abc", false},
		{"non_hn_host", "https://example.com/item?id=1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (hackerNewsAdapter{}).Matches(tt.url); got != tt.want {
				t.Errorf("hackerNewsAdapter{}.Matches(%q) = %v; want %v", tt.url, got, tt.want)
			}
		})
	}
}

// hnAlgoliaURL builds the Algolia API URL for the given item id.
func hnAlgoliaURL(id string) string {
	return hackerNewsItemAPIBase + id
}

// hnFixtureWithComments builds an Algolia item JSON fixture with n top-level
// comments to exercise the maxTopComments bound.
func hnFixtureWithComments(n int) string {
	var b strings.Builder
	b.WriteString(`{"title":"Story With Many Comments","points":10,"author":"alice","url":"","text":"Some story text.","children":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"author":"user%d","text":"Comment number %d."}`, i, i)
	}
	b.WriteString("]}")
	return b.String()
}

const hnStoryWithLinkFixture = `{
  "title": "A Cool Project",
  "points": 250,
  "author": "pg",
  "url": "https://example.com/cool-project",
  "text": "",
  "children": [
    {"author": "alice", "text": "<p>This is <b>alice's</b> comment.</p>"},
    {"author": "bob", "text": "Plain text comment from bob."}
  ]
}`

func TestHackerNewsAdapterFetch(t *testing.T) {
	const url = "https://news.ycombinator.com/item?id=999"

	t.Run("handled_true_with_formatted_markdown", func(t *testing.T) {
		f := stubResponses(t, map[string]*http.Response{
			hnAlgoliaURL("999"): htmlResponse(hnStoryWithLinkFixture),
		}, nil)

		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, url)
		if !handled {
			t.Fatalf("Fetch() handled = false; want true")
		}
		for _, want := range []string{
			"# A Cool Project",
			"HN | 250 points | by pg",
			"Link: https://example.com/cool-project",
			"## Top Comments",
			"**alice**:\nThis is alice's comment.",
			"**bob**:\nPlain text comment from bob.",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("Fetch() out = %q; want it to contain %q", out, want)
			}
		}
	})

	t.Run("post_text_used_instead_of_link_when_present", func(t *testing.T) {
		const fixture = `{"title":"Ask HN: Something","points":5,"author":"carol","url":"","text":"<p>Body text here.</p>","children":[]}`
		f := stubResponses(t, map[string]*http.Response{
			hnAlgoliaURL("1000"): htmlResponse(fixture),
		}, nil)

		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, "https://news.ycombinator.com/item?id=1000")
		if !handled {
			t.Fatalf("Fetch() handled = false; want true")
		}
		if !strings.Contains(out, "Body text here.") {
			t.Errorf("Fetch() out = %q; want the post text", out)
		}
		if strings.Contains(out, "Link:") {
			t.Errorf("Fetch() out = %q; want no Link line when text is present", out)
		}
	})

	t.Run("comments_bounded_to_maxTopComments", func(t *testing.T) {
		fixture := hnFixtureWithComments(maxTopComments + 5)
		f := stubResponses(t, map[string]*http.Response{
			hnAlgoliaURL("2000"): htmlResponse(fixture),
		}, nil)

		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, "https://news.ycombinator.com/item?id=2000")
		if !handled {
			t.Fatalf("Fetch() handled = false; want true")
		}
		if !strings.Contains(out, "user0") || !strings.Contains(out, fmt.Sprintf("user%d", maxTopComments-1)) {
			t.Errorf("Fetch() out = %q; want the first %d comments present", out, maxTopComments)
		}
		if strings.Contains(out, fmt.Sprintf("user%d", maxTopComments)) {
			t.Errorf("Fetch() out = %q; want comments beyond maxTopComments excluded", out)
		}
	})

	t.Run("handled_false_on_non_2xx", func(t *testing.T) {
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}}
		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, url)
		if handled {
			t.Fatalf("Fetch() handled = true, out = %q; want false", out)
		}
	})

	t.Run("handled_false_on_unparseable_body", func(t *testing.T) {
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return htmlResponse("not json"), nil
		}}
		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, url)
		if handled {
			t.Fatalf("Fetch() handled = true, out = %q; want false", out)
		}
	})

	t.Run("handled_false_when_id_extraction_fails", func(t *testing.T) {
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			t.Fatal("f.do should not be called when id extraction fails")
			return nil, nil
		}}
		out, handled := (hackerNewsAdapter{}).Fetch(context.Background(), f, "https://news.ycombinator.com/item")
		if handled {
			t.Fatalf("Fetch() handled = true, out = %q; want false", out)
		}
	})
}
