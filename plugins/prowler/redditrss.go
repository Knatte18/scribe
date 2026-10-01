// redditrss.go implements Reddit's unauthenticated ".rss" read tier: URL canonicalisation, per-IP
// pacing, Atom parsing, and markdown rendering. It is the tier that needs no credentials and no app
// registration -- unlike redditoauth.go's tier 1, it works for every reader with no setup at all.

package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// redditRSSHost is the host every canonical .rss URL is built against, mirroring
// redditOAuthHost's role on the OAuth tier. Matches in reddit.go accepts bare, www, and old
// hosts alike; normalising every one of them onto this single host keeps error strings and
// fixtures singular rather than forking three ways.
const redditRSSHost = "www.reddit.com"

// redditRSSURL rewrites rawURL into the equivalent unauthenticated .rss feed URL: scheme
// forced to https, host normalised to redditRSSHost, any incoming query or fragment
// discarded, and the path made to end in exactly one ".rss" suffix. It returns an error when
// rawURL does not parse or has an empty path, mirroring redditOAuthURL's error shape in
// redditoauth.go.
//
// The path rewrite strips an optional trailing "/", then a trailing ".rss", then another
// optional trailing "/" -- in that order -- before re-appending "/.rss". Each optional strip
// tolerates a stray slash on either side of the ".rss" suffix, so any already-canonical form
// (with or without that slash) collapses back to the bare resource path first. This is what
// makes the function idempotent: feeding its own output back in returns that output
// unchanged. Appending "/.rss" without first stripping an existing ".rss" suffix would instead
// turn an already-".rss" path into "/.rss/.rss" on a second pass.
func redditRSSURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse reddit URL %q: %w", rawURL, err)
	}
	if u.Path == "" {
		return "", fmt.Errorf("reddit URL %q has no path", rawURL)
	}

	u.Scheme = "https"
	u.Host = redditRSSHost
	u.RawQuery = ""
	u.Fragment = ""

	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, ".rss")
	path = strings.TrimSuffix(path, "/")
	u.Path = path + "/.rss"

	return u.String(), nil
}

// redditRSSBaseURL is the base every root-relative href in a Reddit Atom <content> payload is
// resolved against. Reddit emits anchors such as "/r/golang" and "/u/name" with no scheme or
// host at all, and a link rendered without one would be unopenable outside reddit.com.
var redditRSSBaseURL = &url.URL{Scheme: "https", Host: redditRSSHost}

// redditHTMLToMarkdown converts one Reddit Atom <content> payload -- HTML, unlike the OAuth
// tier's Reddit-markdown bodies -- into markdown, then delegates whitespace normalisation and
// tag stripping to the existing htmlToText.
//
// A bare htmlToText call is not enough for this tier: htmlToText is built on goquery's
// .Text(), which discards every href and every block boundary. A comment written
// "[the docs](https://example.com)" would arrive as the bare words "the docs" with the URL
// gone -- and links are the substance of the use case this tier exists for -- and a
// five-paragraph post would arrive as one run-on line. redditHTMLToMarkdown rewrites anchors
// into markdown links and block boundaries into newlines before handing off to htmlToText, so
// both survive; htmlToText itself is untouched, since the generic fetch cascade and the Hacker
// News adapter both depend on its current link-and-block-dropping behaviour.
func redditHTMLToMarkdown(fragment string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + fragment + "</div>"))
	if err != nil {
		return htmlToText(fragment)
	}

	// Anchors become markdown links: "[text](resolved-href)", collapsing to the bare URL
	// when the anchor's text already equals its resolved href, and to the text alone when
	// href is empty.
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		text := strings.TrimSpace(s.Text())

		replacement := text
		if href != "" {
			resolved := redditResolveHref(href)
			if text == resolved {
				replacement = resolved
			} else {
				replacement = "[" + text + "](" + resolved + ")"
			}
		}
		s.ReplaceWithHtml(html.EscapeString(replacement))
	})

	// Block boundaries become newlines, so htmlToText's normalizeWhitespace has something to
	// collapse instead of a single run-on line.
	doc.Find("br").Each(func(_ int, s *goquery.Selection) {
		s.ReplaceWithHtml("\n\n")
	})
	doc.Find("p, blockquote").Each(func(_ int, s *goquery.Selection) {
		s.AppendHtml("\n\n")
	})
	doc.Find("li").Each(func(_ int, s *goquery.Selection) {
		s.PrependHtml("- ")
		s.AppendHtml("\n")
	})

	rewritten, err := doc.Find("div").First().Html()
	if err != nil {
		return htmlToText(fragment)
	}
	return htmlToText(rewritten)
}

// redditResolveHref resolves href against redditRSSBaseURL when it is root-relative, and
// returns it unchanged when it fails to parse -- a best-effort literal is more useful than an
// empty link.
func redditResolveHref(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	return redditRSSBaseURL.ResolveReference(u).String()
}

// redditAtomCategory decodes an Atom <category> element's "term" attribute -- on the
// feed-level element this is the subreddit name.
type redditAtomCategory struct {
	Term string `xml:"term,attr"`
}

// redditAtomAuthor decodes an Atom <author> element's <name> child, the only part of it this
// tier reads.
type redditAtomAuthor struct {
	Name string `xml:"name"`
}

// redditAtomLink decodes an Atom <link> element's "href" attribute, the only part of it this
// tier reads.
type redditAtomLink struct {
	Href string `xml:"href,attr"`
}

// redditAtomFeed decodes an unauthenticated Reddit .rss feed: its title, its feed-level
// <category term=…> (the subreddit name), and every entry in document order.
type redditAtomFeed struct {
	Title    string             `xml:"title"`
	Category redditAtomCategory `xml:"category"`
	Entries  []redditAtomEntry  `xml:"entry"`
}

// redditAtomEntry decodes one Atom <entry>: its title, id, author, HTML content, and its own
// link.
//
// <id> carries Reddit's fullname with its kind prefix -- "t3_" for a post, "t1_" for a
// comment -- and is this tier's only kind discriminator, mirroring redditChild.Kind on the
// OAuth side. Unmapped elements such as media:thumbnail, <updated>, and <published> are
// ignored by encoding/xml and need no fields here.
type redditAtomEntry struct {
	Title   string           `xml:"title"`
	ID      string           `xml:"id"`
	Author  redditAtomAuthor `xml:"author"`
	Content string           `xml:"content"`
	Link    redditAtomLink   `xml:"link"`
}

// parseRedditFeed unmarshals an unauthenticated .rss response body into a redditAtomFeed,
// wrapping any decode failure with the payload's own context.
func parseRedditFeed(body []byte) (redditAtomFeed, error) {
	var feed redditAtomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return redditAtomFeed{}, fmt.Errorf("decode reddit atom feed: %w", err)
	}
	return feed, nil
}

// redditRSSAuthor trims a leading "/u/" or "u/" from an Atom author name. The feed emits
// "/u/username", while formatRedditThread's own rendering already prefixes "u/" itself; left
// untrimmed, a rendered author would read "u//u/username".
func redditRSSAuthor(name string) string {
	name = strings.TrimPrefix(name, "/u/")
	name = strings.TrimPrefix(name, "u/")
	return name
}

// redditRSSSCOffMarker and redditRSSSCOnMarker bound the actual post/comment body within a
// Reddit Atom <content> payload -- everything outside them is Reddit's own rendering
// scaffolding (a thumbnail table, a "submitted by … [link] … [comments]" trailer), never the
// author's own text.
const (
	redditRSSSCOffMarker = "<!-- SC_OFF -->"
	redditRSSSCOnMarker  = "<!-- SC_ON -->"
)

// redditRSSBody returns redditHTMLToMarkdown applied to the span of content strictly between
// redditRSSSCOffMarker and redditRSSSCOnMarker, and the empty string when either marker is
// absent.
//
// It never falls back to the whole content: a link post carries no markers at all, and its
// entire content is a thumbnail <table> plus a "submitted by … [link] … [comments]" trailer --
// a whole-content fallback would render that scaffolding as the post's body. This is the
// common case for link posts in this tier's fixtures, not a rare edge case.
func redditRSSBody(content string) string {
	start := strings.Index(content, redditRSSSCOffMarker)
	if start == -1 {
		return ""
	}
	start += len(redditRSSSCOffMarker)

	end := strings.Index(content[start:], redditRSSSCOnMarker)
	if end == -1 {
		return ""
	}

	return redditHTMLToMarkdown(content[start : start+end])
}

// redditRSSLinkURL parses content with goquery and returns the href of the anchor whose
// trimmed text is exactly "[link]", when that href differs from permalink -- otherwise the
// empty string. The permalink comparison is what stops a self-post's "[comments]"-only
// trailer from rendering as a Link: line pointing back at the page the reader is already on.
func redditRSSLinkURL(content, permalink string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + content + "</div>"))
	if err != nil {
		return ""
	}

	var href string
	doc.Find("a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.TrimSpace(s.Text()) != "[link]" {
			return true
		}
		href, _ = s.Attr("href")
		return false
	})

	if href == "" || href == permalink {
		return ""
	}
	return href
}

// redditPostFromFeed maps feed onto the tier-neutral redditPost representation: the thread
// mapping for this tier. It returns an error when feed.Entries is empty, and an error naming
// the offending id when the first entry's <id> does not start with "t3_" -- a response
// carrying no post is a tier failure rather than an empty document.
//
// Flat is set to true explicitly, never inferred: the feed carries every comment as a sibling
// with no parent reference of any kind, so depth is genuinely unrecoverable and
// "## Top Comments" would be a false claim about entries that are not necessarily top-level.
//
// It never truncates -- the maxTopComments cap lives in formatRedditThread alone, exactly as
// on the OAuth side.
func redditPostFromFeed(feed redditAtomFeed, sourceURL string) (redditPost, error) {
	if len(feed.Entries) == 0 {
		return redditPost{}, fmt.Errorf("reddit atom feed %q has no entries", sourceURL)
	}

	first := feed.Entries[0]
	if !strings.HasPrefix(first.ID, "t3_") {
		return redditPost{}, fmt.Errorf("reddit atom feed %q: first entry id %q is not a post (t3_)", sourceURL, first.ID)
	}

	post := redditPost{
		Title:     first.Title,
		Subreddit: feed.Category.Term,
		Author:    redditRSSAuthor(first.Author.Name),
		Score:     nil,
		Selftext:  redditRSSBody(first.Content),
		URL:       redditRSSLinkURL(first.Content, first.Link.Href),
		Flat:      true,
	}

	for _, entry := range feed.Entries[1:] {
		if !strings.HasPrefix(entry.ID, "t1_") {
			continue
		}
		// An entry whose body comes back empty is skipped rather than rendered as a blank
		// comment -- redditRSSBody returns "" when the SC_OFF/SC_ON markers are absent.
		body := redditRSSBody(entry.Content)
		if body == "" {
			continue
		}
		post.Comments = append(post.Comments, redditComment{
			Author: redditRSSAuthor(entry.Author.Name),
			Score:  nil,
			Body:   body,
		})
	}

	return post, nil
}

// formatRedditListing renders feed as markdown for the non-thread case -- a subreddit or user
// feed with no single post to anchor a redditPost mapping to. It renders an H1 from the feed
// title, a "Source:" line pointing at sourceURL (the caller's original URL, never the derived
// .rss URL, exactly as on the thread branch), and one bullet per entry giving its title,
// author, and link.
func formatRedditListing(feed redditAtomFeed, sourceURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", feed.Title)
	fmt.Fprintf(&b, "Source: %s\n\n", sourceURL)

	for _, entry := range feed.Entries {
		fmt.Fprintf(&b, "- %s by u/%s: %s\n", entry.Title, redditRSSAuthor(entry.Author.Name), entry.Link.Href)
	}

	return strings.TrimRight(b.String(), "\n")
}

// Reddit's unauthenticated .rss endpoint allows roughly one request per 60 seconds per IP, and
// reports the remaining window on every response -- 200, 404, and 429 alike -- in an
// x-ratelimit-reset header. runAll fans out one goroutine per URL, so the limiter below must be
// concurrency-safe, and main builds its fetcher with context.Background(), so it must also be
// bounded independently of the caller's context.
const (
	// redditRSSMinSpacing is the fallback spacing used only when x-ratelimit-reset is absent,
	// empty, or unparseable -- never a floor applied on top of a parsed value. It matches
	// Reddit's documented per-IP budget for this endpoint, roughly one request per 60 seconds.
	redditRSSMinSpacing = 60 * time.Second
	// redditRSSMaxWait is the single deadline covering one whole tier call -- token
	// acquisition, the pacing wait before the first request, and the pacing wait before each
	// retry are all bounded by this one value, not by a fresh budget per step. A per-step
	// budget would let queue time and retry time stack well past what any individual step's
	// own bound suggests, and the sum is what an operator experiences as a hang.
	redditRSSMaxWait = 5 * time.Minute
	// redditRSSLogWaitThreshold is the wait duration at or below which pace logs nothing --
	// short waits are the expected common case and would otherwise spam stderr on every call.
	redditRSSLogWaitThreshold = 2 * time.Second
	// redditRSSMaxAttempts bounds one tier call to one initial attempt plus at most two 429
	// retries.
	redditRSSMaxAttempts = 3
)

// redditRSSWait is the seam a test replaces to avoid a real sleep. The production
// implementation blocks for d or until ctx is cancelled, whichever comes first, returning
// ctx.Err() on cancellation and nil on elapse, and always stopping its timer.
var redditRSSWait = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// redditRSSLogOut is where pace logs a wait that exceeds redditRSSLogWaitThreshold. It is
// os.Stderr in production and must never be os.Stdout: main prints exactly one line to
// stdout -- the output file path -- and the invoking skill wrapper captures that single line,
// so stdout is off limits for this tier.
var redditRSSLogOut io.Writer = os.Stderr

// redditRSSLimiter enforces Reddit's per-IP .rss rate limit across the whole process: exactly
// one caller holds token at a time, and nextAllowed records when the next request may be sent,
// derived from the most recently observed x-ratelimit-reset header.
//
// nextAllowed is owned by whichever goroutine currently holds token -- the channel receive
// that acquires token already establishes a happens-before edge with the previous holder's
// release, so the channel alone provides the mutual exclusion nextAllowed needs; no separate
// mutex is required.
type redditRSSLimiter struct {
	token       chan struct{}
	nextAllowed time.Time
}

// newRedditRSSLimiter allocates a limiter with its single token pre-filled, so the first
// caller to acquire it proceeds immediately.
func newRedditRSSLimiter() *redditRSSLimiter {
	l := &redditRSSLimiter{token: make(chan struct{}, 1)}
	l.token <- struct{}{}
	return l
}

// redditRSSLimit is the one limiter shared by every .rss fetch in this process. Reddit's
// budget is per-IP, not per-URL, so a limiter scoped to a single call site would let sibling
// runAll goroutines overrun the shared window.
var redditRSSLimit = newRedditRSSLimiter()

// reset drains and refills the token and zeroes nextAllowed, returning the limiter to its
// just-constructed state. It exists for tests only, and must never be called concurrently with
// a fetch already in flight.
func (l *redditRSSLimiter) reset() {
	select {
	case <-l.token:
	default:
	}
	l.token <- struct{}{}
	l.nextAllowed = time.Time{}
}

// acquire blocks until the token becomes available, ctx is cancelled, or deadline passes,
// whichever comes first, returning ctx.Err() on cancellation and an error naming how long the
// call waited for the RSS request slot on a deadline timeout.
//
// A sync.Mutex is deliberately not used here: Mutex.Lock is uncancellable and takes no
// deadline, so a goroutine queued behind others could observe neither ctx cancellation nor
// deadline, and the bounds this tier promises would be unimplementable. A select over a
// channel receive, ctx.Done(), and a deadline timer honours all three.
//
// ctx is checked with its own non-blocking select before the three-way select below: Go's
// select chooses pseudo-randomly among every case that is already ready, so when ctx is
// already cancelled and the token happens to be immediately available too (the common case
// right after reset), the three-way select alone would sometimes return the token instead of
// ctx.Err() -- silently letting a cancelled caller proceed to issue a request. Checking
// cancellation first gives it priority over an available token without changing the blocking
// behaviour of the three-way select for the case ctx is still live.
func (l *redditRSSLimiter) acquire(ctx context.Context, deadline time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	wait := deadline.Sub(timeNow())
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-l.token:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("reddit rss rate limiter: waited %s for the request slot", wait)
	}
}

// release returns the token to the limiter, non-blocking. It is always called from a defer in
// the acquiring function, so a cancelled or timed-out caller can never deadlock a later one.
func (l *redditRSSLimiter) release() {
	select {
	case l.token <- struct{}{}:
	default:
	}
}

// pace blocks the token holder -- the only caller entitled to call this method -- until
// nextAllowed, when nextAllowed is still in the future and waiting for it would not cross
// deadline. It returns nil immediately when no wait is needed, an error naming the wait it
// would have needed and the deadline it would have crossed when waiting would exceed deadline
// (without waiting), and otherwise the result of redditRSSWait unchanged.
//
// A wait longer than redditRSSLogWaitThreshold is logged to redditRSSLogOut before waiting, so
// an operator watching stderr sees why a call is slow instead of it looking hung.
func (l *redditRSSLimiter) pace(ctx context.Context, deadline time.Time, rawURL string) error {
	d := l.nextAllowed.Sub(timeNow())
	if d <= 0 {
		return nil
	}
	if timeNow().Add(d).After(deadline) {
		return fmt.Errorf("reddit rss rate limiter: pacing wait of %s for %s would cross deadline %s", d, rawURL, deadline.Format(time.RFC3339))
	}

	if d > redditRSSLogWaitThreshold {
		fmt.Fprintf(redditRSSLogOut, "prowler: reddit rss rate limit, waiting %s before fetching %s\n", d, rawURL)
	}
	return redditRSSWait(ctx, d)
}

// record updates nextAllowed from h's x-ratelimit-reset header. It is called by the token
// holder after every response, success or failure alike, so a failed request still re-arms the
// pacing from the window Reddit actually reported.
func (l *redditRSSLimiter) record(h http.Header) {
	l.nextAllowed = timeNow().Add(redditRSSResetDelay(h))
}

// redditRSSResetDelay parses h's x-ratelimit-reset header with strconv.ParseFloat -- not
// strconv.Atoi, since Reddit float-formats this header family (the captured
// x-ratelimit-remaining: 0.0 proves it), so an integer-only parser would silently degrade to
// the 60-second fallback the day Reddit starts emitting "53.0" -- and, on a successful parse of
// a non-negative value, rounds it up to the nearest whole second.
//
// redditRSSMinSpacing is returned on an absent, empty, unparseable, or negative value: it is a
// missing-header fallback, never a floor applied on top of a parsed value. A clamp such as
// max(reset, redditRSSMinSpacing) would make the header dead code, because every reset value
// ever observed from this endpoint was under 60 seconds.
func redditRSSResetDelay(h http.Header) time.Duration {
	raw := h.Get("x-ratelimit-reset")
	if raw == "" {
		return redditRSSMinSpacing
	}

	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds < 0 {
		return redditRSSMinSpacing
	}
	return time.Duration(math.Ceil(seconds)) * time.Second
}

// fetchRedditRSSFeed issues the one Reddit .rss request this tier makes: it computes the
// call's deadline, acquires the process-wide limiter, paces, requests, retries a 429, records
// the response window, and returns the parsed feed. It sits one level below markdown
// rendering, which is what lets the live integration test in batch 4 read a discovered thread
// link out of a subreddit feed.
func fetchRedditRSSFeed(ctx context.Context, f fetcher, rawURL string) (redditAtomFeed, error) {
	// deadline is computed once, on entry, and bounds every blocking step of this call --
	// token acquisition, the pacing wait before the first request, and the pacing wait before
	// each retry. A per-step budget would let queue time and retry time stack to roughly
	// redditRSSMaxAttempts * redditRSSMaxWait while every individual step stayed "within
	// bounds", and the sum is what an operator experiences as a hang.
	deadline := timeNow().Add(redditRSSMaxWait)

	feedURL, err := redditRSSURL(rawURL)
	if err != nil {
		return redditAtomFeed{}, fmt.Errorf("build reddit rss URL: %w", err)
	}

	if err := redditRSSLimit.acquire(ctx, deadline); err != nil {
		return redditAtomFeed{}, err
	}
	// The token is held across the whole call, including its 429 retries, never released and
	// re-acquired per attempt: releasing between attempts would let a sibling runAll goroutine
	// overtake into a window that is still exhausted, earning another 429 and making the storm
	// worse -- the exact outcome one process-wide token exists to prevent. Siblings queue
	// behind a retrying call for up to its remaining deadline, which redditRSSMaxWait already
	// bounds.
	defer redditRSSLimit.release()

	for attempt := 0; attempt < redditRSSMaxAttempts; attempt++ {
		if err := redditRSSLimit.pace(ctx, deadline, rawURL); err != nil {
			return redditAtomFeed{}, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
		if err != nil {
			return redditAtomFeed{}, fmt.Errorf("build reddit rss request: %w", err)
		}
		req.Header.Set("User-Agent", redditAPIUserAgent())
		req.Header.Set("Accept", "application/atom+xml")

		resp, err := f.do(req)
		if err != nil {
			return redditAtomFeed{}, fmt.Errorf("reddit rss request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return redditAtomFeed{}, fmt.Errorf("read reddit rss response: %w", err)
		}
		// record runs after every response, success or failure alike, so a failed attempt
		// still re-arms the pacing from the window this response actually reported.
		redditRSSLimit.record(resp.Header)

		if resp.StatusCode == http.StatusTooManyRequests {
			// A 429 body is empty (content-length: 0), so the status code is the only
			// signal to key on.
			resetSeconds := redditRSSResetDelay(resp.Header).Seconds()
			if attempt < redditRSSMaxAttempts-1 {
				// record has already re-armed the pacing from this response's own
				// x-ratelimit-reset, so the next iteration's pace waits out the real
				// window.
				continue
			}
			return redditAtomFeed{}, fmt.Errorf("reddit rss request returned status 429, reset in %.0fs", resetSeconds)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if reason, blocked := looksLikeBlockPage(string(body)); blocked {
				return redditAtomFeed{}, fmt.Errorf("reddit rss response looked like a wall (%s)", reason)
			}
			return redditAtomFeed{}, fmt.Errorf("reddit rss request returned status %d", resp.StatusCode)
		}

		feed, err := parseRedditFeed(body)
		if err != nil {
			// Run the body through looksLikeBlockPage first, mirroring
			// fetchRedditOAuthThread's own handling: Reddit has a history of serving
			// HTML walls with 200 statuses from non-HTML endpoints, and an HTML wall
			// would otherwise report as an XML syntax error rather than as a wall.
			if reason, blocked := looksLikeBlockPage(string(body)); blocked {
				return redditAtomFeed{}, fmt.Errorf("reddit rss response looked like a wall (%s) rather than a feed", reason)
			}
			return redditAtomFeed{}, err
		}
		if len(feed.Entries) == 0 {
			// Reddit's not-found response is a valid, entry-less Atom feed; it arrives
			// with a 404 so the status rule above catches it too, but this check must
			// stand on its own because a genuinely empty feed is a failed read either
			// way.
			return redditAtomFeed{}, fmt.Errorf("reddit rss feed %q has no entries", rawURL)
		}

		return feed, nil
	}

	// Unreachable: every branch inside the loop above returns before its final iteration
	// completes. This satisfies the compiler, which cannot itself prove that.
	return redditAtomFeed{}, fmt.Errorf("reddit rss request exhausted %d attempts", redditRSSMaxAttempts)
}

// fetchRedditRSS is the tier entry point redditAdapter.Fetch calls for its RSS tier: it fetches
// and parses the feed via fetchRedditRSSFeed, then discriminates on the parsed feed -- never on
// rawURL alone -- to choose between the thread branch and the listing branch.
//
// The listing branch is never a fall-through for a thread URL: a /comments/ URL whose feed
// carries no post is a broken read -- most likely a removed thread or a wall -- and rendering
// its comments as an anonymous link list would disguise that as a successful fetch.
func fetchRedditRSS(ctx context.Context, f fetcher, rawURL string) (string, error) {
	// Transport, status, decode, and zero-entry failures have already been reported by
	// fetchRedditRSSFeed before any rendering decision is reached.
	feed, err := fetchRedditRSSFeed(ctx, f, rawURL)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse reddit URL %q: %w", rawURL, err)
	}

	if strings.Contains(u.Path, "/comments/") {
		// sourceURL is rawURL -- the caller's original URL, never the derived .rss URL --
		// on both branches, so the rendered Source: line stays a URL a human can open.
		post, err := redditPostFromFeed(feed, rawURL)
		if err != nil {
			return "", err
		}
		return formatRedditThread(post, rawURL), nil
	}

	return formatRedditListing(feed, rawURL), nil
}
