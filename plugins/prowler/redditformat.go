// redditformat.go holds the tier-neutral thread representation and the single markdown renderer
// both Reddit tiers share, so the two tiers match by construction rather than by convention. It also
// maps the OAuth API's decoded listings onto that representation; a future RSS tier maps its own
// source onto the same representation and calls the same renderer.

package main

import (
	"fmt"
	"strings"
)

// redditPost is the tier-neutral representation of one Reddit thread: a post and its top-level
// comments, in a shape formatRedditThread can render regardless of which tier produced it.
type redditPost struct {
	// Title is the post's title, rendered as the document's H1.
	Title string
	// Subreddit is the bare subreddit name, with no "r/" prefix.
	Subreddit string
	// Author is the post's bare username, with no "u/" prefix.
	Author string
	// Score is the post's score. It is a pointer so the renderer can tell "zero points" (a
	// non-nil Score pointing at 0) apart from "this source has no scores" (a nil Score) --
	// a tier that cannot report scores must leave this nil rather than fabricate a zero.
	Score *int
	// Selftext is the post's self-text body, rendered in place of a Link line when non-empty.
	Selftext string
	// URL is the post's link-post target, rendered as a Link line only when Selftext is empty.
	URL string
	// Flat states that this source cannot express reply structure, so the renderer must use
	// the "## Comments" heading instead of "## Top Comments" and every redditComment in
	// Comments has an empty Replies. Each tier sets this explicitly; it is never inferred from
	// the comments themselves.
	Flat bool
	// Comments holds the post's top-level comments, most-relevant (or tier-equivalent) first.
	Comments []redditComment
}

// redditComment is the tier-neutral representation of one Reddit comment, including at most one
// level of replies -- only one level of Replies is ever rendered, regardless of how deep the
// source thread actually nests.
type redditComment struct {
	// Author is the comment's bare username, with no "u/" prefix.
	Author string
	// Score is the comment's score, nil when the source tier cannot report one -- see
	// redditPost.Score for why this is a pointer rather than a zero-value sentinel.
	Score *int
	// Body is the comment's text.
	Body string
	// Replies holds this comment's direct replies. A reply's own Replies is never populated by
	// a mapping function and never consulted by the renderer.
	Replies []redditComment
}

// formatRedditThread renders post into markdown: an H1 title; a metadata line naming the
// subreddit, score (when known), and author; a "Source:" line pointing at sourceURL; the post's
// selftext, or a link line when there is no selftext but there is a URL; and, only when post has
// at least one comment, a comments heading followed by up to maxTopComments of them, each with up
// to maxTopComments of its own replies rendered one level deep as blockquotes.
//
// The comments heading is "## Top Comments" when post.Flat is false and "## Comments" when it is
// true -- chosen from Flat alone, never from whether the comments happen to carry replies, since a
// genuinely reply-less OAuth thread is common and must still render "## Top Comments".
//
// Comment and selftext bodies are rendered unchanged, not run through htmlToText -- Reddit's own
// markdown bodies (OAuth) and htmlToText's already-converted output (RSS) both arrive here as
// plain text, so this function never touches HTML itself.
func formatRedditThread(post redditPost, sourceURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", post.Title)

	if post.Score != nil {
		fmt.Fprintf(&b, "Reddit | r/%s | %d points | by u/%s\n\n", post.Subreddit, *post.Score, post.Author)
	} else {
		fmt.Fprintf(&b, "Reddit | r/%s | by u/%s\n\n", post.Subreddit, post.Author)
	}
	fmt.Fprintf(&b, "Source: %s\n\n", sourceURL)

	if post.Selftext != "" {
		b.WriteString(post.Selftext)
		b.WriteString("\n\n")
	} else if post.URL != "" {
		fmt.Fprintf(&b, "Link: %s\n\n", post.URL)
	}

	if len(post.Comments) > 0 {
		if post.Flat {
			b.WriteString("## Comments\n\n")
		} else {
			b.WriteString("## Top Comments\n\n")
		}

		comments := post.Comments
		if len(comments) > maxTopComments {
			comments = comments[:maxTopComments]
		}
		for _, c := range comments {
			if c.Score != nil {
				fmt.Fprintf(&b, "**u/%s** (%d points):\n%s\n\n", c.Author, *c.Score, c.Body)
			} else {
				fmt.Fprintf(&b, "**u/%s**:\n%s\n\n", c.Author, c.Body)
			}

			replies := c.Replies
			if len(replies) > maxTopComments {
				replies = replies[:maxTopComments]
			}
			for _, reply := range replies {
				fmt.Fprintf(&b, "> **u/%s**: %s\n", reply.Author, reply.Body)
			}
			if len(replies) > 0 {
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// redditPostFromListings maps a decoded OAuth thread response -- listings, in the order
// oauth.reddit.com's comments endpoint returns them: the post listing, then the comments listing
// -- onto the tier-neutral redditPost representation. It returns a non-nil error when listings is
// empty or its first listing has no "t3" child, since a response carrying no post is a tier
// failure rather than an empty document.
//
// It never truncates: every comment and every reply it parses is handed to the returned
// redditPost, so maxTopComments capping lives in exactly one place -- formatRedditThread.
func redditPostFromListings(listings []redditListing) (redditPost, error) {
	if len(listings) == 0 {
		return redditPost{}, fmt.Errorf("reddit thread response had no listings")
	}

	var thing redditThing
	var foundPost bool
	for _, child := range listings[0].Data.Children {
		if child.Kind == "t3" {
			thing = child.Data
			foundPost = true
			break
		}
	}
	if !foundPost {
		return redditPost{}, fmt.Errorf("reddit thread response's first listing had no post (t3) child")
	}

	score := thing.Score
	post := redditPost{
		Title:     thing.Title,
		Subreddit: thing.Subreddit,
		Author:    thing.Author,
		Score:     &score,
		Selftext:  thing.Selftext,
		URL:       thing.URL,
		Flat:      false,
	}

	if len(listings) > 1 {
		for _, child := range listings[1].Data.Children {
			if child.Kind != "t1" {
				// Skips "more" pagination placeholders and anything else that
				// is not a rendered comment.
				continue
			}
			post.Comments = append(post.Comments, redditCommentFromChild(child))
		}
	}

	return post, nil
}

// redditCommentFromChild maps one OAuth "t1" child onto a redditComment, walking its Replies one
// level deep via redditReplies. Replies whose Kind is not "t1" (pagination placeholders and
// anything else) are skipped, matching redditPostFromListings' top-level filtering.
func redditCommentFromChild(child redditChild) redditComment {
	score := child.Data.Score
	comment := redditComment{
		Author: child.Data.Author,
		Score:  &score,
		Body:   child.Data.Body,
	}

	for _, reply := range redditReplies(child.Data.Replies) {
		if reply.Kind != "t1" {
			continue
		}
		// Only one level of replies is mapped: reply.Data.Replies (a reply's
		// own replies) is deliberately never consulted.
		replyScore := reply.Data.Score
		comment.Replies = append(comment.Replies, redditComment{
			Author: reply.Data.Author,
			Score:  &replyScore,
			Body:   reply.Data.Body,
		})
	}

	return comment
}
