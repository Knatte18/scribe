// redditformat_test.go exercises the shared representation and renderer: redditPostFromListings'
// mapping from decoded OAuth listings, and formatRedditThread's markdown rendering, including the
// byte-for-byte OAuth golden regression that proves the tier-neutral refactor changed nothing
// observable on the credentialed path. No network call.

package main

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
)

// updateRedditGolden regenerates testdata/reddit-thread-golden.md from the
// current formatter's output instead of comparing against it. It is never
// set in CI; a human sets it deliberately, via
// `go -C plugins/prowler test -run TestFormatRedditThread_GoldenOAuthOutput . -args -update-golden`,
// after confirming a formatter change is intended.
var updateRedditGolden = flag.Bool("update-golden", false, "regenerate testdata/reddit-thread-golden.md from the current formatter's output")

// testdata/reddit-thread.json is hand-authored rather than captured from a
// live response, because oauth.reddit.com cannot be read without
// credentials that do not exist yet at this point in the plan; batch 4's
// live integration test is what validates the real response shape. It is
// structurally faithful to Reddit's documented comments-endpoint Listing
// shape: a two-element array of [post listing, comments listing].
func TestFormatRedditThread(t *testing.T) {
	data := readTestdataFile(t, "reddit-thread.json")

	var listings []redditListing
	if err := json.Unmarshal([]byte(data), &listings); err != nil {
		t.Fatalf("json.Unmarshal(reddit-thread.json) error = %v", err)
	}

	post, err := redditPostFromListings(listings)
	if err != nil {
		t.Fatalf("redditPostFromListings() error = %v; want nil", err)
	}
	out := formatRedditThread(post, "https://oauth.reddit.com/r/golang/comments/abc123")

	if !strings.Contains(out, "What is the idiomatic way to handle errors in Go?") {
		t.Errorf("formatRedditThread() out missing title:\n%s", out)
	}
	if !strings.Contains(out, "I've been writing Go for a few months") {
		t.Errorf("formatRedditThread() out missing selftext:\n%s", out)
	}

	if topLevelCount := strings.Count(out, "** ("); topLevelCount != maxTopComments {
		t.Errorf("formatRedditThread() top-level comment count = %d; want %d", topLevelCount, maxTopComments)
	}
	// c20 and c21 fall beyond the cap; the "more" child sits before them in
	// the fixture, so if it were mistakenly counted toward the cap it would
	// have displaced a real comment earlier than c20 -- these two absences
	// together prove the more child contributed nothing and the cap landed
	// in the right place.
	if strings.Contains(out, "top_author_20") || strings.Contains(out, "top_author_21") {
		t.Errorf("formatRedditThread() out contains a comment beyond maxTopComments:\n%s", out)
	}

	if replyCount := strings.Count(out, "> **u/capreply"); replyCount != maxTopComments {
		t.Errorf("formatRedditThread() reply count for the over-capped comment = %d; want %d", replyCount, maxTopComments)
	}

	if strings.Contains(out, "A reply to a reply") {
		t.Errorf("formatRedditThread() out contains a reply's own nested reply, which must not render:\n%s", out)
	}

	t.Run("zero_comment_thread", func(t *testing.T) {
		const fixture = `[{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"title":"A quiet post","selftext":"Nobody has replied yet.","author":"solo","subreddit":"golang","score":1,"url":"","replies":""}}]}}]`
		var listings []redditListing
		if err := json.Unmarshal([]byte(fixture), &listings); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		post, err := redditPostFromListings(listings)
		if err != nil {
			t.Fatalf("redditPostFromListings() error = %v; want nil", err)
		}
		out := formatRedditThread(post, "https://oauth.reddit.com/r/golang/comments/quiet")
		if out == "" {
			t.Error("formatRedditThread() out is empty; want a non-empty result")
		}
		if strings.Contains(out, "## Top Comments") {
			t.Errorf("formatRedditThread() out contains a Top Comments heading with zero comments:\n%s", out)
		}
	})
}

// TestFormatRedditThread_GoldenOAuthOutput compares formatRedditThread's
// current output for testdata/reddit-thread.json against the byte-for-byte
// golden file committed at testdata/reddit-thread-golden.md. This is the
// regression that proves the tier-neutral-representation refactor changes
// nothing observable on the credentialed OAuth path: the golden file was
// captured before that refactor and must still match after it.
func TestFormatRedditThread_GoldenOAuthOutput(t *testing.T) {
	data := readTestdataFile(t, "reddit-thread.json")

	var listings []redditListing
	if err := json.Unmarshal([]byte(data), &listings); err != nil {
		t.Fatalf("json.Unmarshal(reddit-thread.json) error = %v", err)
	}

	const sourceURL = "https://www.reddit.com/r/golang/comments/abc123/idiomatic_errors/"
	const goldenPath = "testdata/reddit-thread-golden.md"

	post, err := redditPostFromListings(listings)
	if err != nil {
		t.Fatalf("redditPostFromListings() error = %v; want nil", err)
	}
	out := formatRedditThread(post, sourceURL)

	if *updateRedditGolden {
		if err := os.WriteFile(goldenPath, []byte(out), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", goldenPath, err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", goldenPath, err)
	}

	if out != string(want) {
		t.Errorf("formatRedditThread() output does not match golden file %s; want the two byte-for-byte identical.\n"+
			"If this difference is intended, re-run with -args -update-golden after confirming it.\ngot:\n%s\nwant:\n%s",
			goldenPath, out, want)
	}
}

// intPtr returns a pointer to v, for building redditPost/redditComment
// values whose Score field must distinguish "no score" (nil) from "a score
// of zero" (a non-nil pointer to 0).
func intPtr(v int) *int {
	return &v
}

// TestFormatRedditThread_NilScore covers the pointer-vs-sentinel distinction
// that formatRedditThread's nil-Score branches exist for: a nil Score omits
// the points segment entirely, while a Score pointing at 0 still renders
// "0 points" -- the case that fails if anyone reaches for a zero-value
// sentinel instead of a pointer.
func TestFormatRedditThread_NilScore(t *testing.T) {
	t.Run("nil_post_score_omits_points_segment", func(t *testing.T) {
		post := redditPost{Title: "t", Subreddit: "golang", Author: "op"}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "Reddit | r/golang | by u/op") {
			t.Errorf("formatRedditThread() out = %q; want metadata line with no points segment", out)
		}
		if strings.Contains(out, "points") {
			t.Errorf("formatRedditThread() out = %q; want no \"points\" text for a nil Score", out)
		}
	})

	t.Run("zero_post_score_renders_zero_points", func(t *testing.T) {
		post := redditPost{Title: "t", Subreddit: "golang", Author: "op", Score: intPtr(0)}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "Reddit | r/golang | 0 points | by u/op") {
			t.Errorf("formatRedditThread() out = %q; want a metadata line with \"0 points\"", out)
		}
	})

	t.Run("nil_comment_score_omits_points_segment", func(t *testing.T) {
		post := redditPost{
			Title: "t", Subreddit: "golang", Author: "op", Flat: false,
			Comments: []redditComment{{Author: "commenter", Body: "hello"}},
		}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "**u/commenter**:\nhello") {
			t.Errorf("formatRedditThread() out = %q; want a comment header with no points segment", out)
		}
	})

	t.Run("zero_comment_score_renders_zero_points", func(t *testing.T) {
		post := redditPost{
			Title: "t", Subreddit: "golang", Author: "op", Flat: false,
			Comments: []redditComment{{Author: "commenter", Score: intPtr(0), Body: "hello"}},
		}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "**u/commenter** (0 points):\nhello") {
			t.Errorf("formatRedditThread() out = %q; want a comment header with \"0 points\"", out)
		}
	})
}

// TestFormatRedditThread_HeadingDiscriminator covers that the comments
// heading is chosen from post.Flat alone, never inferred from whether the
// comments happen to carry replies -- a genuinely reply-less OAuth thread
// (Flat: false, every comment's Replies nil) is common and must still
// render "## Top Comments".
func TestFormatRedditThread_HeadingDiscriminator(t *testing.T) {
	tests := []struct {
		name        string
		flat        bool
		wantHeading string
	}{
		{"flat_false_renders_top_comments", false, "## Top Comments"},
		{"flat_true_renders_comments", true, "## Comments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post := redditPost{
				Title: "t", Subreddit: "golang", Author: "op", Flat: tt.flat,
				Comments: []redditComment{{Author: "c", Body: "b"}},
			}
			out := formatRedditThread(post, "https://example.com")
			if !strings.Contains(out, tt.wantHeading) {
				t.Errorf("formatRedditThread() out = %q; want heading %q", out, tt.wantHeading)
			}
		})
	}

	t.Run("flat_false_with_all_nil_replies_still_top_comments", func(t *testing.T) {
		post := redditPost{
			Title: "t", Subreddit: "golang", Author: "op", Flat: false,
			Comments: []redditComment{
				{Author: "c1", Body: "b1"},
				{Author: "c2", Body: "b2"},
			},
		}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "## Top Comments") {
			t.Errorf("formatRedditThread() out = %q; want \"## Top Comments\" even though no comment has replies", out)
		}
	})
}

// TestFormatRedditThread_CapPlacement covers that maxTopComments is applied
// by the formatter -- at the top-level comment slice and at each comment's
// reply slice -- and never by redditPostFromListings, which must hand over
// every comment and reply it parses untruncated so the cap lives in exactly
// one place.
func TestFormatRedditThread_CapPlacement(t *testing.T) {
	t.Run("top_level_comments_capped_at_maxTopComments", func(t *testing.T) {
		var comments []redditComment
		for i := 0; i < maxTopComments+5; i++ {
			comments = append(comments, redditComment{Author: "c", Body: "b"})
		}
		post := redditPost{Title: "t", Subreddit: "golang", Author: "op", Comments: comments}
		out := formatRedditThread(post, "https://example.com")
		if got := strings.Count(out, "**u/c**:"); got != maxTopComments {
			t.Errorf("formatRedditThread() rendered %d top-level comments; want %d", got, maxTopComments)
		}
	})

	t.Run("replies_capped_at_maxTopComments", func(t *testing.T) {
		var replies []redditComment
		for i := 0; i < maxTopComments+5; i++ {
			replies = append(replies, redditComment{Author: "r", Body: "reply body"})
		}
		post := redditPost{
			Title: "t", Subreddit: "golang", Author: "op",
			Comments: []redditComment{{Author: "c", Body: "b", Replies: replies}},
		}
		out := formatRedditThread(post, "https://example.com")
		if got := strings.Count(out, "> **u/r**: reply body"); got != maxTopComments {
			t.Errorf("formatRedditThread() rendered %d replies; want %d", got, maxTopComments)
		}
	})

	t.Run("redditPostFromListings_does_not_truncate", func(t *testing.T) {
		var commentChildren []redditChild
		for i := 0; i < maxTopComments+5; i++ {
			commentChildren = append(commentChildren, redditChild{
				Kind: "t1",
				Data: redditThing{Author: "c", Body: "b"},
			})
		}
		var postListing, commentsListing redditListing
		postListing.Data.Children = []redditChild{{Kind: "t3", Data: redditThing{Title: "t", Author: "op"}}}
		commentsListing.Data.Children = commentChildren
		listings := []redditListing{postListing, commentsListing}

		post, err := redditPostFromListings(listings)
		if err != nil {
			t.Fatalf("redditPostFromListings() error = %v; want nil", err)
		}
		if got, want := len(post.Comments), maxTopComments+5; got != want {
			t.Errorf("redditPostFromListings() len(Comments) = %d; want %d (untruncated)", got, want)
		}
	})
}

// TestFormatRedditThread_LinkRendering covers the Selftext-vs-URL choice: a
// link-post (empty Selftext, non-empty URL) renders a "Link:" line, while a
// self-post renders the selftext and never a "Link:" line even when URL is
// also set.
func TestFormatRedditThread_LinkRendering(t *testing.T) {
	t.Run("empty_selftext_renders_link_line", func(t *testing.T) {
		post := redditPost{Title: "t", Subreddit: "golang", Author: "op", URL: "https://example.com/target"}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "Link: https://example.com/target") {
			t.Errorf("formatRedditThread() out = %q; want a Link line", out)
		}
	})

	t.Run("non_empty_selftext_never_renders_link_line", func(t *testing.T) {
		post := redditPost{
			Title: "t", Subreddit: "golang", Author: "op",
			Selftext: "the post body", URL: "https://example.com/target",
		}
		out := formatRedditThread(post, "https://example.com")
		if !strings.Contains(out, "the post body") {
			t.Errorf("formatRedditThread() out = %q; want the selftext rendered", out)
		}
		if strings.Contains(out, "Link:") {
			t.Errorf("formatRedditThread() out = %q; want no Link line when Selftext is non-empty", out)
		}
	})
}

// TestRedditPostFromListings_KindFiltering covers redditPostFromListings'
// error paths and its "more"-placeholder filtering at both the top-level
// comment list and each comment's reply list.
func TestRedditPostFromListings_KindFiltering(t *testing.T) {
	t.Run("empty_listings_yields_error", func(t *testing.T) {
		_, err := redditPostFromListings(nil)
		if err == nil {
			t.Fatal("redditPostFromListings() error = nil; want non-nil for an empty listings slice")
		}
	})

	t.Run("first_listing_with_no_post_yields_error", func(t *testing.T) {
		var listing redditListing
		listing.Data.Children = []redditChild{{Kind: "t1", Data: redditThing{Author: "not-a-post"}}}
		_, err := redditPostFromListings([]redditListing{listing})
		if err == nil {
			t.Fatal("redditPostFromListings() error = nil; want non-nil when the first listing has no t3 child")
		}
	})

	t.Run("more_placeholders_dropped_at_top_level_and_reply_level", func(t *testing.T) {
		fixture := `[
			{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"title":"t","author":"op","subreddit":"golang","score":1}}]}},
			{"kind":"Listing","data":{"children":[
				{"kind":"more","data":{"id":"x","children":["a","b"]}},
				{"kind":"t1","data":{"author":"c1","body":"comment one","score":5,"replies":{"kind":"Listing","data":{"children":[
					{"kind":"more","data":{"id":"y","children":["c"]}},
					{"kind":"t1","data":{"author":"r1","body":"reply one","score":1,"replies":""}}
				]}}}}
			]}}
		]`
		var listings []redditListing
		if err := json.Unmarshal([]byte(fixture), &listings); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		post, err := redditPostFromListings(listings)
		if err != nil {
			t.Fatalf("redditPostFromListings() error = %v; want nil", err)
		}
		if len(post.Comments) != 1 {
			t.Fatalf("redditPostFromListings() len(Comments) = %d; want 1 (the \"more\" placeholder dropped)", len(post.Comments))
		}
		if len(post.Comments[0].Replies) != 1 {
			t.Errorf("redditPostFromListings() len(Comments[0].Replies) = %d; want 1 (the reply-level \"more\" placeholder dropped)", len(post.Comments[0].Replies))
		}
	})
}
