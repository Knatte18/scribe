// redditrss_test.go exercises the RSS tier offline, no network: URL canonicalisation (this
// file), HTML-to-markdown conversion, and Atom parsing/mapping are all added here card by
// card as batch 2 proceeds.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRedditRSSURL covers redditRSSURL's host/scheme normalisation, query/fragment
// stripping, and idempotent ".rss" suffixing across Reddit's three host forms and both
// slash-terminated and unterminated paths.
func TestRedditRSSURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "bare_host",
			in:   "https://reddit.com/r/golang",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "www_host",
			in:   "https://www.reddit.com/r/golang",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "old_host",
			in:   "https://old.reddit.com/r/golang",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "http_scheme_forced_to_https",
			in:   "http://www.reddit.com/r/golang",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "path_with_trailing_slash",
			in:   "https://www.reddit.com/r/golang/",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "path_without_trailing_slash",
			in:   "https://www.reddit.com/r/golang",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "query_string_dropped",
			in:   "https://www.reddit.com/r/golang?sort=new",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "fragment_dropped",
			in:   "https://www.reddit.com/r/golang#comments",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "already_rss_with_trailing_slash_unchanged",
			in:   "https://www.reddit.com/r/golang/.rss/",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "already_rss_without_trailing_slash_unchanged",
			in:   "https://www.reddit.com/r/golang/.rss",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name: "subreddit_path",
			in:   "https://www.reddit.com/r/golang/",
			want: "https://www.reddit.com/r/golang/.rss",
		},
		{
			name:    "empty_path_yields_error",
			in:      "https://www.reddit.com",
			wantErr: true,
		},
		{
			name:    "unparseable_url_yields_error",
			in:      "://bad",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := redditRSSURL(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("redditRSSURL(%q) error = nil; want non-nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("redditRSSURL(%q) error = %v; want nil", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("redditRSSURL(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}

	// The success cases above are also the idempotence corpus: feeding each one's own
	// output back through redditRSSURL must return that output unchanged, proving the
	// ".rss"-stripping step in redditRSSURL prevents "/.rss/.rss" on a second pass.
	t.Run("idempotence", func(t *testing.T) {
		for _, tt := range tests {
			if tt.wantErr {
				continue
			}
			t.Run(tt.name, func(t *testing.T) {
				first, err := redditRSSURL(tt.in)
				if err != nil {
					t.Fatalf("redditRSSURL(%q) error = %v; want nil", tt.in, err)
				}
				second, err := redditRSSURL(first)
				if err != nil {
					t.Fatalf("redditRSSURL(%q) error = %v; want nil", first, err)
				}
				if second != first {
					t.Errorf("redditRSSURL(%q) = %q; want %q (idempotent)", first, second, first)
				}
			})
		}
	})
}

// TestRedditHTMLToMarkdown covers redditHTMLToMarkdown's three rewrites -- anchors to
// markdown links, block boundaries to newlines -- and proves htmlToText itself is untouched.
func TestRedditHTMLToMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
		want     string
	}{
		{
			name:     "absolute_href_anchor_becomes_markdown_link",
			fragment: `<p>See <a href="https://example.com/docs">the docs</a> for more.</p>`,
			want:     "[the docs](https://example.com/docs)",
		},
		{
			name:     "relative_href_absolutized",
			fragment: `<p>Check out <a href="/r/golang">/r/golang</a>.</p>`,
			want:     "[/r/golang](https://www.reddit.com/r/golang)",
		},
		{
			name:     "anchor_text_equal_to_href_renders_bare_url",
			fragment: `<p>Website: <a href="https://pingularity.dev">https://pingularity.dev</a></p>`,
			want:     "https://pingularity.dev",
		},
		{
			name:     "li_items_render_as_bullets",
			fragment: `<ul><li>first item</li><li>second item</li></ul>`,
			want:     "- first item\n- second item",
		},
		{
			name:     "nested_markup_inside_anchor_text",
			fragment: `<p><a href="https://example.com"><strong>bold</strong> text</a></p>`,
			want:     "[bold text](https://example.com)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redditHTMLToMarkdown(tt.fragment)
			if !strings.Contains(got, tt.want) {
				t.Errorf("redditHTMLToMarkdown(%q) = %q; want it to contain %q", tt.fragment, got, tt.want)
			}
		})
	}

	t.Run("paragraph_and_br_produce_blank_line_breaks", func(t *testing.T) {
		got := redditHTMLToMarkdown(`<p>first paragraph</p><p>second paragraph</p>`)
		if !strings.Contains(got, "first paragraph\n\nsecond paragraph") {
			t.Errorf("redditHTMLToMarkdown() = %q; want a blank-line break between paragraphs", got)
		}

		got = redditHTMLToMarkdown(`line one<br>line two`)
		if !strings.Contains(got, "line one\n\nline two") {
			t.Errorf("redditHTMLToMarkdown() = %q; want a blank-line break at <br>", got)
		}
	})

	t.Run("empty_href_renders_text_alone", func(t *testing.T) {
		got := redditHTMLToMarkdown(`<p>an <a href="">anchor with no href</a> here</p>`)
		if !strings.Contains(got, "anchor with no href") {
			t.Errorf("redditHTMLToMarkdown() = %q; want the anchor's bare text", got)
		}
		if strings.Contains(got, "[") || strings.Contains(got, "]") {
			t.Errorf("redditHTMLToMarkdown() = %q; want no markdown link brackets for an empty href", got)
		}
	})

	// Regression: a real comment body pulled from the committed thread fixture must still
	// carry its external link after conversion.
	t.Run("real_fixture_comment_keeps_its_external_link", func(t *testing.T) {
		data, err := os.ReadFile("testdata/reddit-thread.rss")
		if err != nil {
			t.Fatalf("os.ReadFile(reddit-thread.rss) error = %v", err)
		}
		// The fixture is raw Atom XML, so its <content> payload is entity-escaped; unescape
		// once before hunting for the literal SC_OFF/SC_ON markers. Several entries in this
		// fixture carry their own SC_OFF/SC_ON span, so scan all of them for the one
		// containing the known external link rather than assuming it is the first.
		text := html.UnescapeString(string(data))
		var fragment string
		for searchFrom := 0; ; {
			start := strings.Index(text[searchFrom:], "<!-- SC_OFF -->")
			if start == -1 {
				break
			}
			start += searchFrom
			end := strings.Index(text[start:], "<!-- SC_ON -->")
			if end == -1 {
				break
			}
			end += start + len("<!-- SC_ON -->")
			span := text[start:end]
			if strings.Contains(span, "pingularity.dev") {
				fragment = span
				break
			}
			searchFrom = end
		}
		if fragment == "" {
			t.Fatalf("testdata/reddit-thread.rss: expected fixture setup changed -- no SC_OFF/SC_ON span containing pingularity.dev found")
		}

		got := redditHTMLToMarkdown(fragment)
		if !strings.Contains(got, "https://pingularity.dev") {
			t.Errorf("redditHTMLToMarkdown() on the real fixture comment = %q; want it to still contain https://pingularity.dev", got)
		}
	})

	// Proves the shared helper is provably untouched: htmlToText's own behaviour on an
	// anchor-bearing fragment is unchanged -- the URL is still dropped.
	t.Run("htmlToText_itself_still_drops_the_href", func(t *testing.T) {
		got := htmlToText(`<p>See <a href="https://example.com/docs">the docs</a> for more.</p>`)
		if strings.Contains(got, "https://example.com/docs") {
			t.Errorf("htmlToText() = %q; want the href still dropped (htmlToText must stay untouched)", got)
		}
		if !strings.Contains(got, "the docs") {
			t.Errorf("htmlToText() = %q; want the anchor's text preserved", got)
		}
	})
}

// parseRedditFeedTestdata reads and parses a testdata Atom fixture, failing the test on
// either error.
func parseRedditFeedTestdata(t *testing.T, name string) redditAtomFeed {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", name, err)
	}
	feed, err := parseRedditFeed(data)
	if err != nil {
		t.Fatalf("parseRedditFeed(%q) error = %v; want nil", name, err)
	}
	return feed
}

// TestParseRedditFeed covers parseRedditFeed's decoding of the feed-level category and every
// entry's id, against the real committed thread fixture.
func TestParseRedditFeed(t *testing.T) {
	feed := parseRedditFeedTestdata(t, "reddit-thread.rss")

	if len(feed.Entries) != 5 {
		t.Fatalf("parseRedditFeed() len(Entries) = %d; want 5", len(feed.Entries))
	}
	if got, want := feed.Entries[0].ID, "t3_1vxc255"; got != want {
		t.Errorf("parseRedditFeed() Entries[0].ID = %q; want %q", got, want)
	}
	if got, want := feed.Category.Term, "golang"; got != want {
		t.Errorf("parseRedditFeed() Category.Term = %q; want %q", got, want)
	}
	for i, entry := range feed.Entries[1:] {
		if !strings.HasPrefix(entry.ID, "t1_") {
			t.Errorf("parseRedditFeed() Entries[%d].ID = %q; want a t1_ prefix", i+1, entry.ID)
		}
	}
}

// TestRedditPostFromFeed covers redditPostFromFeed's mapping of a full thread feed onto
// redditPost, against the real committed thread fixture.
func TestRedditPostFromFeed(t *testing.T) {
	feed := parseRedditFeedTestdata(t, "reddit-thread.rss")

	post, err := redditPostFromFeed(feed, "https://www.reddit.com/r/golang/comments/1vxc255/small_projects/")
	if err != nil {
		t.Fatalf("redditPostFromFeed() error = %v; want nil", err)
	}

	if got, want := post.Title, "Small Projects"; got != want {
		t.Errorf("redditPostFromFeed() Title = %q; want %q", got, want)
	}
	if got, want := post.Subreddit, "golang"; got != want {
		t.Errorf("redditPostFromFeed() Subreddit = %q; want %q", got, want)
	}
	if got, want := post.Author, "AutoModerator"; got != want {
		t.Errorf("redditPostFromFeed() Author = %q; want %q", got, want)
	}
	if post.Score != nil {
		t.Errorf("redditPostFromFeed() Score = %v; want nil", *post.Score)
	}
	if !strings.Contains(post.Selftext, "This is the weekly thread for Small Projects.") {
		t.Errorf("redditPostFromFeed() Selftext = %q; want it to contain the post's real body text", post.Selftext)
	}
	for _, scaffold := range []string{"submitted by", "[link]", "[comments]"} {
		if strings.Contains(post.Selftext, scaffold) {
			t.Errorf("redditPostFromFeed() Selftext = %q; want no trailer text %q", post.Selftext, scaffold)
		}
	}
	if !post.Flat {
		t.Error("redditPostFromFeed() Flat = false; want true")
	}

	if got, want := len(post.Comments), 4; got != want {
		t.Fatalf("redditPostFromFeed() len(Comments) = %d; want %d", got, want)
	}
	wantComments := []struct {
		author string
	}{
		{"cansofgrease"},
		{"realPanditJi"},
		{"SovereignZ3r0"},
		{"mrehanabbasi"},
	}
	for i, want := range wantComments {
		c := post.Comments[i]
		if c.Author != want.author {
			t.Errorf("redditPostFromFeed() Comments[%d].Author = %q; want %q", i, c.Author, want.author)
		}
		if c.Body == "" {
			t.Errorf("redditPostFromFeed() Comments[%d].Body is empty; want non-empty", i)
		}
		if c.Score != nil {
			t.Errorf("redditPostFromFeed() Comments[%d].Score = %v; want nil", i, *c.Score)
		}
		if c.Replies != nil {
			t.Errorf("redditPostFromFeed() Comments[%d].Replies = %v; want nil", i, c.Replies)
		}
	}
}

// findRedditAtomEntryByID locates the entry with the given id within a parsed feed's entries,
// failing the test if it is absent. Tests use this rather than a fixed index so a fixture
// reorder cannot silently break a test that is looking for a specific entry.
func findRedditAtomEntryByID(t *testing.T, feed redditAtomFeed, id string) redditAtomEntry {
	t.Helper()
	for _, entry := range feed.Entries {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("no entry with id %q found in feed", id)
	return redditAtomEntry{}
}

// TestRedditPostFromFeed_MarkerAbsentLinkPost covers the common case a whole-content fallback
// would get wrong: a link post's <content> carries no SC_OFF/SC_ON markers at all, since its
// entire content is a thumbnail table plus a "submitted by … [link] … [comments]" trailer.
func TestRedditPostFromFeed_MarkerAbsentLinkPost(t *testing.T) {
	listing := parseRedditFeedTestdata(t, "reddit-listing.rss")
	entry := findRedditAtomEntryByID(t, listing, "t3_1vx8uvc")

	feed := redditAtomFeed{Title: listing.Title, Category: listing.Category, Entries: []redditAtomEntry{entry}}
	post, err := redditPostFromFeed(feed, "https://www.reddit.com/r/golang/comments/1vx8uvc/excessive_nil_pointer_checks_in_go/")
	if err != nil {
		t.Fatalf("redditPostFromFeed() error = %v; want nil", err)
	}

	if post.Selftext != "" {
		t.Errorf("redditPostFromFeed() Selftext = %q; want empty (no SC_OFF/SC_ON markers)", post.Selftext)
	}
	const wantURL = "https://konradreiche.com/blog/excessive-nil-pointer-checks-in-go/"
	if post.URL != wantURL {
		t.Errorf("redditPostFromFeed() URL = %q; want %q (the external [link] href, not the permalink)", post.URL, wantURL)
	}

	out := formatRedditThread(post, "https://www.reddit.com/r/golang/comments/1vx8uvc/excessive_nil_pointer_checks_in_go/")
	if !strings.Contains(out, "Link: "+wantURL) {
		t.Errorf("formatRedditThread() out = %q; want a Link: line for %q", out, wantURL)
	}
	for _, scaffold := range []string{"submitted by", "[link]", "[comments]"} {
		if strings.Contains(out, scaffold) {
			t.Errorf("formatRedditThread() out = %q; want no trailer text %q", out, scaffold)
		}
	}
}

// TestRedditPostFromFeed_SelfPostURLSuppression covers the permalink comparison in
// redditRSSLinkURL: a self-post's [link] anchor points at its own permalink, so the mapped
// URL must be empty and formatRedditThread must render the selftext with no Link: line.
func TestRedditPostFromFeed_SelfPostURLSuppression(t *testing.T) {
	feed := parseRedditFeedTestdata(t, "reddit-thread.rss")
	const sourceURL = "https://www.reddit.com/r/golang/comments/1vxc255/small_projects/"

	post, err := redditPostFromFeed(feed, sourceURL)
	if err != nil {
		t.Fatalf("redditPostFromFeed() error = %v; want nil", err)
	}
	if post.URL != "" {
		t.Errorf("redditPostFromFeed() URL = %q; want empty (self-post [link] points at its own permalink)", post.URL)
	}

	out := formatRedditThread(post, sourceURL)
	if !strings.Contains(out, "This is the weekly thread for Small Projects.") {
		t.Errorf("formatRedditThread() out = %q; want the selftext rendered", out)
	}
	if strings.Contains(out, "Link:") {
		t.Errorf("formatRedditThread() out = %q; want no Link: line for a self-post", out)
	}
}

// TestRedditPostFromFeed_ZeroEntries covers the not-found fixture: a feed with no entries at
// all must yield an error, not a zero-value post.
func TestRedditPostFromFeed_ZeroEntries(t *testing.T) {
	feed := parseRedditFeedTestdata(t, "reddit-rss-notfound.rss")
	if len(feed.Entries) != 0 {
		t.Fatalf("parseRedditFeed(reddit-rss-notfound.rss) len(Entries) = %d; want 0", len(feed.Entries))
	}

	_, err := redditPostFromFeed(feed, "https://www.reddit.com/r/announcements/.rss")
	if err == nil {
		t.Fatal("redditPostFromFeed() error = nil; want non-nil for a zero-entry feed")
	}
}

// TestRedditPostFromFeed_NonT3FirstEntry covers redditPostFromFeed's kind-discriminator
// check: a first entry whose id does not start with "t3_" must yield an error naming that id.
func TestRedditPostFromFeed_NonT3FirstEntry(t *testing.T) {
	feed := redditAtomFeed{
		Title:    "some feed",
		Category: redditAtomCategory{Term: "golang"},
		Entries:  []redditAtomEntry{{ID: "t1_something", Title: "not a post"}},
	}

	_, err := redditPostFromFeed(feed, "https://www.reddit.com/r/golang/.rss")
	if err == nil {
		t.Fatal("redditPostFromFeed() error = nil; want non-nil when the first entry is not a t3_ post")
	}
	if !strings.Contains(err.Error(), "t1_something") {
		t.Errorf("redditPostFromFeed() error = %q; want it to name the offending id %q", err, "t1_something")
	}
}

// TestFormatRedditListing covers the non-thread rendering path, against the real committed
// listing fixture.
func TestFormatRedditListing(t *testing.T) {
	feed := parseRedditFeedTestdata(t, "reddit-listing.rss")
	const sourceURL = "https://www.reddit.com/r/golang/"

	out := formatRedditListing(feed, sourceURL)

	if !strings.Contains(out, "# "+feed.Title) {
		t.Errorf("formatRedditListing() out = %q; want an H1 with the feed title %q", out, feed.Title)
	}
	if !strings.Contains(out, "Source: "+sourceURL) {
		t.Errorf("formatRedditListing() out = %q; want a Source: line with %q", out, sourceURL)
	}
	for _, entry := range feed.Entries {
		want := fmt.Sprintf("- %s by u/%s: %s", entry.Title, redditRSSAuthor(entry.Author.Name), entry.Link.Href)
		if !strings.Contains(out, want) {
			t.Errorf("formatRedditListing() out missing bullet %q\nfull output:\n%s", want, out)
		}
	}
}

// redditRSSLimiterStub is the handle stubRedditRSSLimiter hands back to a test: the wait
// durations redditRSSWait was asked for, a fake clock the test can advance, and the log buffer
// redditRSSLogOut was redirected to. All fields are read through its methods rather than
// directly, since the limiter's own goroutines read the clock concurrently with a test driving
// it.
type redditRSSLimiterStub struct {
	mu    sync.Mutex
	waits []time.Duration
	now   time.Time
	log   *bytes.Buffer
}

// advance moves the fake clock forward by d.
func (s *redditRSSLimiterStub) advance(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = s.now.Add(d)
}

// clock returns the fake clock's current time; it is installed as the timeNow seam.
func (s *redditRSSLimiterStub) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// recordedWaits returns the durations redditRSSWait was asked to wait, in call order.
func (s *redditRSSLimiterStub) recordedWaits() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]time.Duration, len(s.waits))
	copy(out, s.waits)
	return out
}

// stubRedditRSSLimiter replaces redditRSSWait with a no-op that records the durations it was
// asked to wait and returns nil (or ctx.Err() when ctx is already cancelled), points timeNow at
// a controllable fake clock the test can advance, redirects redditRSSLogOut at a bytes.Buffer
// the test can read, calls redditRSSLimit.reset(), and registers a t.Cleanup restoring all
// four so no test leaks state into another.
//
// Every untagged test reaching the RSS tier must call this as its first statement: the limiter
// is a process-wide singleton, and stubResponses builds responses with no x-ratelimit-reset
// header, so the second unstubbed RSS test in the process would sleep 60 real seconds under the
// production redditRSSWait.
func stubRedditRSSLimiter(t *testing.T) *redditRSSLimiterStub {
	t.Helper()

	stub := &redditRSSLimiterStub{
		now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		log: &bytes.Buffer{},
	}

	realWait := redditRSSWait
	realTimeNow := timeNow
	realLogOut := redditRSSLogOut

	redditRSSWait = func(ctx context.Context, d time.Duration) error {
		stub.mu.Lock()
		stub.waits = append(stub.waits, d)
		stub.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timeNow = stub.clock
	redditRSSLogOut = stub.log

	redditRSSLimit.reset()

	t.Cleanup(func() {
		redditRSSWait = realWait
		timeNow = realTimeNow
		redditRSSLogOut = realLogOut
	})

	return stub
}

// TestRedditRSSLogOutDefaultsToStderr guards that redditRSSLogOut is never routed to stdout in
// production: main prints exactly one line to stdout (the output file path), and the invoking
// skill wrapper captures that single line. It runs without stubRedditRSSLimiter deliberately,
// so it observes the package's real, un-redirected default.
func TestRedditRSSLogOutDefaultsToStderr(t *testing.T) {
	if redditRSSLogOut != io.Writer(os.Stderr) {
		t.Errorf("redditRSSLogOut = %v; want os.Stderr", redditRSSLogOut)
	}
	if redditRSSLogOut == io.Writer(os.Stdout) {
		t.Error("redditRSSLogOut must never be os.Stdout")
	}
}

// TestRedditRSSResetDelay covers redditRSSResetDelay's float parsing, its round-up-to-whole-
// seconds rule, and every fallback case.
func TestRedditRSSResetDelay(t *testing.T) {
	tests := []struct {
		name      string
		setHeader bool
		val       string
		want      time.Duration
	}{
		// Below redditRSSMinSpacing: this is the row that fails if anyone reintroduces a
		// max(reset, 60s) clamp.
		{"below_min_spacing_parses_verbatim", true, "3", 3 * time.Second},
		// Float-formatted: the row that fails if anyone reaches for strconv.Atoi.
		{"float_formatted_value", true, "53.0", 53 * time.Second},
		{"fractional_value_rounds_up", true, "12.3", 13 * time.Second},
		{"missing_header_falls_back", false, "", redditRSSMinSpacing},
		{"empty_header_falls_back", true, "", redditRSSMinSpacing},
		{"non_numeric_header_falls_back", true, "not-a-number", redditRSSMinSpacing},
		{"negative_value_falls_back", true, "-5", redditRSSMinSpacing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.setHeader {
				h.Set("x-ratelimit-reset", tt.val)
			}
			got := redditRSSResetDelay(h)
			if got != tt.want {
				t.Errorf("redditRSSResetDelay(%q) = %s; want %s", tt.val, got, tt.want)
			}
		})
	}
}

// TestRedditRSSLimiter covers acquire/pace/release/record's blocking, deadline, cancellation,
// logging, and serialisation behaviour directly against redditRSSLimit -- all via the stubbed
// clock and wait, so every case completes in milliseconds.
func TestRedditRSSLimiter(t *testing.T) {
	t.Run("first_acquisition_has_no_wait", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
			t.Fatalf("acquire() error = %v; want nil", err)
		}
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com"); err != nil {
			t.Fatalf("pace() error = %v; want nil", err)
		}
		redditRSSLimit.release()

		if got := stub.recordedWaits(); len(got) != 0 {
			t.Errorf("recordedWaits() = %v; want none for the first acquisition", got)
		}
	})

	t.Run("second_acquisition_waits_until_nextAllowed", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
			t.Fatalf("first acquire() error = %v; want nil", err)
		}
		redditRSSLimit.record(http.Header{}) // no header: falls back to redditRSSMinSpacing
		redditRSSLimit.release()

		if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
			t.Fatalf("second acquire() error = %v; want nil", err)
		}
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com"); err != nil {
			t.Fatalf("second pace() error = %v; want nil", err)
		}
		redditRSSLimit.release()

		waits := stub.recordedWaits()
		if len(waits) != 1 {
			t.Fatalf("recordedWaits() = %v; want exactly one wait for the second call", waits)
		}
		if waits[0] != redditRSSMinSpacing {
			t.Errorf("recordedWaits()[0] = %s; want %s", waits[0], redditRSSMinSpacing)
		}
	})

	t.Run("pace_wait_equals_parsed_header_verbatim", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
			t.Fatalf("acquire() error = %v; want nil", err)
		}
		h := http.Header{}
		h.Set("x-ratelimit-reset", "3")
		redditRSSLimit.record(h)
		redditRSSLimit.release()

		if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
			t.Fatalf("acquire() error = %v; want nil", err)
		}
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com"); err != nil {
			t.Fatalf("pace() error = %v; want nil", err)
		}
		redditRSSLimit.release()

		waits := stub.recordedWaits()
		if len(waits) != 1 || waits[0] != 3*time.Second {
			t.Fatalf("recordedWaits() = %v; want exactly [3s] -- this is the case that fails if a max(reset, 60s) clamp is reintroduced", waits)
		}
	})

	t.Run("cancelled_context_aborts_acquire_and_pace", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		deadline := stub.clock().Add(redditRSSMaxWait)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Drain the token so acquire has nothing to receive and must observe ctx.Done()
		// instead.
		<-redditRSSLimit.token
		if err := redditRSSLimit.acquire(ctx, deadline); !errors.Is(err, context.Canceled) {
			t.Errorf("acquire() error = %v; want context.Canceled", err)
		}
		redditRSSLimit.token <- struct{}{}

		redditRSSLimit.nextAllowed = stub.clock().Add(time.Minute)
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com"); !errors.Is(err, context.Canceled) {
			t.Errorf("pace() error = %v; want context.Canceled", err)
		}
	})

	t.Run("acquire_times_out_at_deadline", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(10 * time.Millisecond)

		<-redditRSSLimit.token // drain so acquire has nothing to receive
		t.Cleanup(func() { redditRSSLimit.token <- struct{}{} })

		if err := redditRSSLimit.acquire(ctx, deadline); err == nil {
			t.Fatal("acquire() error = nil; want non-nil once the token never arrives before deadline")
		}
	})

	t.Run("pace_fails_at_deadline_without_waiting", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)
		redditRSSLimit.nextAllowed = deadline.Add(time.Minute)

		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com"); err == nil {
			t.Fatal("pace() error = nil; want non-nil when the pacing wait would cross deadline")
		}
		if waits := stub.recordedWaits(); len(waits) != 0 {
			t.Errorf("recordedWaits() = %v; want none -- pace must fail without waiting", waits)
		}
	})

	t.Run("concurrent_callers_serialise", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		const goroutines = 10
		var active int32
		var maxActive int32
		var wg sync.WaitGroup
		errs := make(chan error, goroutines)
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
					errs <- err
					return
				}
				defer redditRSSLimit.release()

				n := atomic.AddInt32(&active, 1)
				for {
					m := atomic.LoadInt32(&maxActive)
					if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				atomic.AddInt32(&active, -1)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		if got := atomic.LoadInt32(&maxActive); got != 1 {
			t.Errorf("max concurrent token holders = %d; want exactly 1", got)
		}
	})

	t.Run("timed_out_caller_returns_the_token_for_a_later_caller", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()

		<-redditRSSLimit.token // simulate the token already held elsewhere
		shortDeadline := stub.clock().Add(5 * time.Millisecond)
		if err := redditRSSLimit.acquire(ctx, shortDeadline); err == nil {
			t.Fatal("acquire() error = nil; want non-nil (no token available before the short deadline)")
		}
		redditRSSLimit.token <- struct{}{} // the "elsewhere" holder finishes and releases

		longDeadline := stub.clock().Add(redditRSSMaxWait)
		if err := redditRSSLimit.acquire(ctx, longDeadline); err != nil {
			t.Fatalf("second acquire() error = %v; want nil -- a timed-out caller must not deadlock a later one", err)
		}
		redditRSSLimit.release()
	})

	t.Run("wait_at_threshold_logs_nothing", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		redditRSSLimit.nextAllowed = stub.clock().Add(redditRSSLogWaitThreshold)
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com/at-threshold"); err != nil {
			t.Fatalf("pace() error = %v; want nil", err)
		}
		if stub.log.Len() != 0 {
			t.Errorf("log = %q; want empty for a wait at the threshold", stub.log.String())
		}
	})

	t.Run("wait_one_second_over_threshold_logs_exactly_one_line_naming_seconds_and_url", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		ctx := context.Background()
		deadline := stub.clock().Add(redditRSSMaxWait)

		redditRSSLimit.nextAllowed = stub.clock().Add(redditRSSLogWaitThreshold + time.Second)
		if err := redditRSSLimit.pace(ctx, deadline, "https://example.com/over-threshold"); err != nil {
			t.Fatalf("pace() error = %v; want nil", err)
		}
		logged := stub.log.String()
		if strings.Count(logged, "\n") != 1 {
			t.Fatalf("log = %q; want exactly one line", logged)
		}
		if !strings.Contains(logged, "https://example.com/over-threshold") {
			t.Errorf("log = %q; want it to name the URL", logged)
		}
		if !strings.Contains(logged, "3s") {
			t.Errorf("log = %q; want it to name the seconds waited", logged)
		}
	})
}

// rssResponse builds a canned *http.Response for RSS-tier tests.
func rssResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

// TestFetchRedditRSSFeed covers fetchRedditRSSFeed's request shape, its failure detection
// (transport error, non-2xx status, block-page walls, zero-entry feeds), its 429 retry budget,
// the shared call deadline across retries, token-holding across a retry sequence, and context
// cancellation.
func TestFetchRedditRSSFeed(t *testing.T) {
	const rawURL = "https://www.reddit.com/r/golang/comments/1vxc255/small_projects/"

	t.Run("200_returns_parsed_feed_with_expected_request_shape", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		body := readTestdataFile(t, "reddit-thread.rss")
		wantURL, err := redditRSSURL(rawURL)
		if err != nil {
			t.Fatalf("redditRSSURL() error = %v", err)
		}

		var gotReq *http.Request
		f := fetcher{do: func(req *http.Request) (*http.Response, error) {
			gotReq = req
			return rssResponse(200, nil, body), nil
		}}

		feed, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err != nil {
			t.Fatalf("fetchRedditRSSFeed() error = %v; want nil", err)
		}
		if len(feed.Entries) != 5 {
			t.Errorf("fetchRedditRSSFeed() len(Entries) = %d; want 5", len(feed.Entries))
		}
		if gotReq.URL.String() != wantURL {
			t.Errorf("request URL = %q; want %q", gotReq.URL.String(), wantURL)
		}
		if got := gotReq.Header.Get("User-Agent"); got != defaultRedditAPIUserAgent {
			t.Errorf("request User-Agent = %q; want %q", got, defaultRedditAPIUserAgent)
		}
		if got := gotReq.Header.Get("Accept"); got != "application/atom+xml" {
			t.Errorf("request Accept = %q; want %q", got, "application/atom+xml")
		}
		if got := gotReq.Header.Get("Accept-Encoding"); got != "" {
			t.Errorf("request Accept-Encoding = %q; want empty (unset)", got)
		}
	})

	t.Run("transport_error_names_the_cause", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}}
		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("fetchRedditRSSFeed() error = %v; want it to name the transport failure", err)
		}
	})

	t.Run("status_500_names_the_cause", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return rssResponse(500, nil, "boom"), nil
		}}
		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Errorf("fetchRedditRSSFeed() error = %v; want it to name status 500", err)
		}
	})

	t.Run("status_404_names_the_cause", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return rssResponse(404, nil, "not found"), nil
		}}
		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("fetchRedditRSSFeed() error = %v; want it to name status 404", err)
		}
	})

	t.Run("200_block_page_body_reports_wall_not_xml_error", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		blockHTML := readTestdataFile(t, "reddit-block-page.html")
		wantReason, blocked := looksLikeBlockPage(blockHTML)
		if !blocked {
			t.Fatal("fixture reddit-block-page.html does not trip looksLikeBlockPage")
		}
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return rssResponse(200, nil, blockHTML), nil
		}}

		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil {
			t.Fatal("fetchRedditRSSFeed() error = nil; want non-nil")
		}
		if !strings.Contains(err.Error(), wantReason) {
			t.Errorf("fetchRedditRSSFeed() error = %q; want it to mention %q", err, wantReason)
		}
		lowered := strings.ToLower(err.Error())
		if strings.Contains(lowered, "eof") || strings.Contains(lowered, "syntax error") {
			t.Errorf("fetchRedditRSSFeed() error = %q; want a wall reason, not an XML decode error", err)
		}
	})

	t.Run("403_block_page_body_reports_wall_not_bare_status", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		blockHTML := readTestdataFile(t, "reddit-block-page.html")
		wantReason, blocked := looksLikeBlockPage(blockHTML)
		if !blocked {
			t.Fatal("fixture reddit-block-page.html does not trip looksLikeBlockPage")
		}
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return rssResponse(403, nil, blockHTML), nil
		}}

		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil {
			t.Fatal("fetchRedditRSSFeed() error = nil; want non-nil")
		}
		if !strings.Contains(err.Error(), wantReason) {
			t.Errorf("fetchRedditRSSFeed() error = %q; want it to mention %q rather than the bare status", err, wantReason)
		}
	})

	t.Run("200_notfound_fixture_reports_zero_entries", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		body := readTestdataFile(t, "reddit-rss-notfound.rss")
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			return rssResponse(200, nil, body), nil
		}}

		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil {
			t.Fatal("fetchRedditRSSFeed() error = nil; want non-nil for a zero-entry feed")
		}
	})

	t.Run("429_retry_budget", func(t *testing.T) {
		t.Run("two_429s_then_200_succeeds_with_three_requests", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			body := readTestdataFile(t, "reddit-thread.rss")
			resetHeader := http.Header{}
			resetHeader.Set("x-ratelimit-reset", "1")
			var requestCount int
			f := fetcher{do: func(*http.Request) (*http.Response, error) {
				requestCount++
				if requestCount <= 2 {
					return rssResponse(429, resetHeader, ""), nil
				}
				return rssResponse(200, nil, body), nil
			}}

			feed, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
			if err != nil {
				t.Fatalf("fetchRedditRSSFeed() error = %v; want nil", err)
			}
			if len(feed.Entries) != 5 {
				t.Errorf("fetchRedditRSSFeed() len(Entries) = %d; want 5", len(feed.Entries))
			}
			if requestCount != 3 {
				t.Errorf("request count = %d; want exactly 3", requestCount)
			}
		})

		t.Run("three_429s_fails_naming_reset_seconds_with_three_requests", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			resetHeader := http.Header{}
			resetHeader.Set("x-ratelimit-reset", "7")
			var requestCount int
			f := fetcher{do: func(*http.Request) (*http.Response, error) {
				requestCount++
				return rssResponse(429, resetHeader, ""), nil
			}}

			_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
			if err == nil {
				t.Fatal("fetchRedditRSSFeed() error = nil; want non-nil after exhausting the retry budget")
			}
			if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "7") {
				t.Errorf("fetchRedditRSSFeed() error = %q; want it to name status 429 and the reset seconds 7", err)
			}
			if requestCount != 3 {
				t.Errorf("request count = %d; want exactly 3 (redditRSSMaxAttempts)", requestCount)
			}
		})
	})

	t.Run("deadline_enforced_across_retries_not_per_step", func(t *testing.T) {
		stub := stubRedditRSSLimiter(t)
		resetHeader := http.Header{}
		resetHeader.Set("x-ratelimit-reset", "1")
		var requestCount int
		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			requestCount++
			// Advance the clock past redditRSSMaxWait between attempts, so the second
			// pace call must observe the deadline has already passed rather than
			// waiting out the retry budget.
			stub.advance(redditRSSMaxWait + time.Second)
			return rssResponse(429, resetHeader, ""), nil
		}}

		_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
		if err == nil {
			t.Fatal("fetchRedditRSSFeed() error = nil; want non-nil once the clock has passed the call deadline")
		}
		if requestCount != 1 {
			t.Errorf("request count = %d; want exactly 1 -- the deadline must stop the retry before a second request is issued", requestCount)
		}
	})

	t.Run("token_held_across_retries_blocks_a_concurrent_second_caller", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		resetHeader := http.Header{}
		resetHeader.Set("x-ratelimit-reset", "1")
		body := readTestdataFile(t, "reddit-thread.rss")

		firstStarted := make(chan struct{})
		releaseFirst := make(chan struct{})
		var firstRequests int32
		var secondRequests int32

		firstDone := make(chan error, 1)
		go func() {
			f := fetcher{do: func(*http.Request) (*http.Response, error) {
				n := atomic.AddInt32(&firstRequests, 1)
				if n == 1 {
					close(firstStarted)
					<-releaseFirst
					return rssResponse(429, resetHeader, ""), nil
				}
				return rssResponse(200, nil, body), nil
			}}
			_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
			firstDone <- err
		}()

		<-firstStarted
		secondDone := make(chan error, 1)
		go func() {
			f := fetcher{do: func(*http.Request) (*http.Response, error) {
				atomic.AddInt32(&secondRequests, 1)
				return rssResponse(200, nil, body), nil
			}}
			_, err := fetchRedditRSSFeed(context.Background(), f, rawURL)
			secondDone <- err
		}()

		// While the first caller sits inside its blocked retry, the second caller must
		// not have issued any request yet.
		time.Sleep(20 * time.Millisecond)
		if got := atomic.LoadInt32(&secondRequests); got != 0 {
			t.Errorf("second caller's request count = %d; want 0 while the first caller still holds the token", got)
		}

		close(releaseFirst)
		if err := <-firstDone; err != nil {
			t.Fatalf("first fetchRedditRSSFeed() error = %v; want nil", err)
		}
		if err := <-secondDone; err != nil {
			t.Fatalf("second fetchRedditRSSFeed() error = %v; want nil", err)
		}
	})

	t.Run("cancelled_context_returns_ctx_err_without_issuing_a_request", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		f := fetcher{do: func(*http.Request) (*http.Response, error) {
			t.Fatal("transport must not be invoked once ctx is already cancelled")
			return nil, nil
		}}

		_, err := fetchRedditRSSFeed(ctx, f, rawURL)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("fetchRedditRSSFeed() error = %v; want context.Canceled", err)
		}
	})
}

// TestFetchRedditRSS covers the tier entry point's thread/listing branch selection, its fixed
// evaluation order, and Source: provenance on both branches.
func TestFetchRedditRSS(t *testing.T) {
	t.Run("comments_url_renders_thread_shape", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		const rawURL = "https://www.reddit.com/r/golang/comments/1vxc255/small_projects/"
		body := readTestdataFile(t, "reddit-thread.rss")
		wantURL, err := redditRSSURL(rawURL)
		if err != nil {
			t.Fatalf("redditRSSURL() error = %v", err)
		}
		f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(body)}, nil)

		out, err := fetchRedditRSS(context.Background(), f, rawURL)
		if err != nil {
			t.Fatalf("fetchRedditRSS() error = %v; want nil", err)
		}
		if !strings.HasPrefix(out, "# Small Projects") {
			t.Errorf("fetchRedditRSS() out = %q; want it to start with the post title", out)
		}
		if strings.Contains(out, "points") {
			t.Errorf("fetchRedditRSS() out = %q; want no points segment (the RSS tier reports no score)", out)
		}
		if !strings.Contains(out, "## Comments") {
			t.Errorf("fetchRedditRSS() out = %q; want a \"## Comments\" heading", out)
		}
		for _, author := range []string{"cansofgrease", "realPanditJi", "SovereignZ3r0", "mrehanabbasi"} {
			if !strings.Contains(out, author) {
				t.Errorf("fetchRedditRSS() out missing comment author %q", author)
			}
		}
	})

	t.Run("non_comments_url_renders_listing_shape_not_thread_shape", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		const rawURL = "https://www.reddit.com/r/golang/"
		body := readTestdataFile(t, "reddit-listing.rss")
		wantURL, err := redditRSSURL(rawURL)
		if err != nil {
			t.Fatalf("redditRSSURL() error = %v", err)
		}
		f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(body)}, nil)

		out, err := fetchRedditRSS(context.Background(), f, rawURL)
		if err != nil {
			t.Fatalf("fetchRedditRSS() error = %v; want nil", err)
		}
		if strings.Contains(out, "## Comments") {
			t.Errorf("fetchRedditRSS() out = %q; want no \"## Comments\" heading for a listing", out)
		}
		feed := parseRedditFeedTestdata(t, "reddit-listing.rss")
		if got, want := strings.Count(out, "\n- "), len(feed.Entries); got != want {
			t.Errorf("fetchRedditRSS() out has %d bullets; want %d (one per entry)", got, want)
		}
	})

	t.Run("evaluation_order", func(t *testing.T) {
		// A feed whose first entry is t1_ (a comment, not a post) built directly, rather
		// than relying on a fixture, so this test exercises exactly the evaluation-order
		// rule under test.
		const feedXML = `<?xml version="1.0" encoding="UTF-8"?>` +
			`<feed xmlns="http://www.w3.org/2005/Atom">` +
			`<category term="golang"/>` +
			`<entry><id>t1_abc</id><title>a comment, not a post</title></entry>` +
			`</feed>`

		t.Run("comments_url_errors_rather_than_falling_through_to_listing", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			const rawURL = "https://www.reddit.com/r/golang/comments/abc/some_title/"
			wantURL, err := redditRSSURL(rawURL)
			if err != nil {
				t.Fatalf("redditRSSURL() error = %v", err)
			}
			f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(feedXML)}, nil)

			_, err = fetchRedditRSS(context.Background(), f, rawURL)
			if err == nil {
				t.Fatal("fetchRedditRSS() error = nil; want non-nil when the /comments/ feed's first entry is not a post")
			}
		})

		t.Run("non_comments_url_renders_the_same_feed_as_a_listing", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			const rawURL = "https://www.reddit.com/r/golang/"
			wantURL, err := redditRSSURL(rawURL)
			if err != nil {
				t.Fatalf("redditRSSURL() error = %v", err)
			}
			f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(feedXML)}, nil)

			out, err := fetchRedditRSS(context.Background(), f, rawURL)
			if err != nil {
				t.Fatalf("fetchRedditRSS() error = %v; want nil (a non-/comments/ URL renders any feed as a listing)", err)
			}
			if !strings.Contains(out, "a comment, not a post") {
				t.Errorf("fetchRedditRSS() out = %q; want the listing to include the entry", out)
			}
		})
	})

	t.Run("source_provenance", func(t *testing.T) {
		t.Run("thread_branch_carries_the_original_url_no_rss_suffix", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			const rawURL = "https://www.reddit.com/r/golang/comments/1vxc255/small_projects/"
			body := readTestdataFile(t, "reddit-thread.rss")
			wantURL, err := redditRSSURL(rawURL)
			if err != nil {
				t.Fatalf("redditRSSURL() error = %v", err)
			}
			f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(body)}, nil)

			out, err := fetchRedditRSS(context.Background(), f, rawURL)
			if err != nil {
				t.Fatalf("fetchRedditRSS() error = %v; want nil", err)
			}
			if !strings.Contains(out, "Source: "+rawURL) {
				t.Errorf("fetchRedditRSS() out = %q; want a Source: line with the original URL %q", out, rawURL)
			}
			if strings.Contains(out, ".rss") {
				t.Errorf("fetchRedditRSS() out = %q; want no \".rss\" suffix anywhere in the output", out)
			}
		})

		t.Run("listing_branch_carries_the_original_url_no_rss_suffix", func(t *testing.T) {
			stubRedditRSSLimiter(t)
			const rawURL = "https://www.reddit.com/r/golang/"
			body := readTestdataFile(t, "reddit-listing.rss")
			wantURL, err := redditRSSURL(rawURL)
			if err != nil {
				t.Fatalf("redditRSSURL() error = %v", err)
			}
			f := stubResponses(t, map[string]*http.Response{wantURL: htmlResponse(body)}, nil)

			out, err := fetchRedditRSS(context.Background(), f, rawURL)
			if err != nil {
				t.Fatalf("fetchRedditRSS() error = %v; want nil", err)
			}
			if !strings.Contains(out, "Source: "+rawURL) {
				t.Errorf("fetchRedditRSS() out = %q; want a Source: line with the original URL %q", out, rawURL)
			}
			if strings.Contains(out, ".rss") {
				t.Errorf("fetchRedditRSS() out = %q; want no \".rss\" suffix anywhere in the output", out)
			}
		})
	})
}
