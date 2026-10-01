// hackernews.go implements the Hacker News site adapter: it fetches a discussion thread's clean,
// structured JSON from the community-run Algolia API (https://hn.algolia.com/api/v1/items/{id})
// rather than scraping HN's own server-rendered HTML, giving a second adapter strategy distinct
// from Reddit's structured-source approach (OAuth JSON or `.rss` Atom feeds).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// hackerNewsItemAPIBase is the Algolia HN API endpoint an item id is
// appended to, e.g. "https://hn.algolia.com/api/v1/items/12345".
const hackerNewsItemAPIBase = "https://hn.algolia.com/api/v1/items/"

// hackerNewsItem models fields needed from Algolia HN API item responses.
// Children is the nested comment tree (only top level rendered here).
type hackerNewsItem struct {
	Title    string           `json:"title"`
	Points   int              `json:"points"`
	Author   string           `json:"author"`
	URL      string           `json:"url"`
	Text     string           `json:"text"`
	Children []hackerNewsItem `json:"children"`
}

// hackerNewsAdapter is the siteAdapter for Hacker News: it matches
// individual item ("story") pages and formats them via the Algolia API.
type hackerNewsAdapter struct{}

// Matches reports whether url is a Hacker News item page (news.ycombinator.com/item?id=N, any
// scheme or optional "www."
// prefix).
func (hackerNewsAdapter) Matches(rawURL string) bool {
	_, ok := hackerNewsItemID(rawURL)
	return ok
}

// Fetch retrieves the Algolia API representation and formats it into markdown.
// Reports handled=false if the id can't be extracted, request fails, response is
// non-2xx/unparseable, or the item has neither title nor text.
func (hackerNewsAdapter) Fetch(ctx context.Context, f fetcher, rawURL string) (out string, handled bool) {
	id, ok := hackerNewsItemID(rawURL)
	if !ok {
		return "", false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hackerNewsItemAPIBase+id, nil)
	if err != nil {
		return "", false
	}
	// No special User-Agent: the Algolia API is a public, unauthenticated
	// endpoint with no bot-detection to work around, unlike Reddit's HTML.

	resp, err := f.do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	var item hackerNewsItem
	if err := json.Unmarshal(body, &item); err != nil {
		return "", false
	}

	// A well-formed but empty item (e.g. a deleted/dangling id) has nothing
	// worth showing; let the caller fall through instead of returning a
	// near-blank result.
	if item.Title == "" && item.Text == "" {
		return "", false
	}

	return formatHackerNewsItem(item), true
}

// hackerNewsItemID extracts the numeric item id from a Hacker News item URL
// (news.ycombinator.com/item?id=N). Reports false for other HN URLs or non-HN URLs.
func hackerNewsItemID(rawURL string) (id string, ok bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	if strings.TrimPrefix(parsed.Hostname(), "www.") != "news.ycombinator.com" {
		return "", false
	}
	if strings.Trim(parsed.Path, "/") != "item" {
		return "", false
	}

	idParam := parsed.Query().Get("id")
	if idParam == "" {
		return "", false
	}
	// Algolia items are keyed by the same numeric id HN itself uses, so a
	// non-numeric id can never resolve to a real item.
	if _, err := strconv.Atoi(idParam); err != nil {
		return "", false
	}
	return idParam, true
}

// formatHackerNewsItem renders a Hacker News item into markdown with title,
// source/points/author line, post text or link, and top-level comments.
func formatHackerNewsItem(item hackerNewsItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", item.Title)
	fmt.Fprintf(&b, "HN | %d points | by %s\n\n", item.Points, item.Author)

	if item.Text != "" {
		// Algolia's text field is server-supplied HTML, not plain text.
		b.WriteString(htmlToText(item.Text))
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "Link: %s\n\n", item.URL)
	}

	topComments := item.Children
	if len(topComments) > maxTopComments {
		topComments = topComments[:maxTopComments]
	}

	if len(topComments) > 0 {
		b.WriteString("## Top Comments\n\n")
		for _, c := range topComments {
			fmt.Fprintf(&b, "**%s**:\n%s\n\n", c.Author, htmlToText(c.Text))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
