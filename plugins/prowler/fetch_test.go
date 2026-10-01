// fetch_test.go exercises the static-fetch extraction cascade with a stubbed fetcher.do (canned
// responses keyed by request URL) and a stubbed fetcher.browser.
// No network, no Chrome.

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

// stubResponses builds a fetcher with canned URL-based responses and a stub browser.
func stubResponses(t *testing.T, responses map[string]*http.Response, browser func(ctx context.Context, url string) (string, bool)) fetcher {
	t.Helper()
	respond := func(req *http.Request) (*http.Response, error) {
		resp, ok := responses[req.URL.String()]
		if !ok {
			t.Fatalf("unexpected request URL: %s", req.URL.String())
		}
		return resp, nil
	}
	return fetcher{
		do:      respond,
		browser: browser,
	}
}

func htmlResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const readableArticleHTML = `<html><head><title>Ignored</title></head><body>
<article><h1>A Real Article</h1>` +
	`<p>This is a long paragraph of genuinely readable article content that easily exceeds one hundred characters in length, so Readability should treat it as the main article body.</p>` +
	`</article></body></html>`

const shortArticleHTML = `<html><body><article><h1>Tiny</h1><p>Too short.</p></article></body></html>`

// noArticleButLongBodyHTML hides text behind display:none to test Readability
// failure with body text fallback.
const noArticleButLongBodyHTML = `<html><body><div style="display:none">` +
	`This page has no article/semantic structure at all, just a long stretch of plain body text that is not wrapped in anything Readability recognizes as an article, but is still well over one hundred characters of readable prose for the body-text fallback to pick up.` +
	`</div></body></html>`

// noArticleShortBodyHTML hides short text to test browser fallback when both
// Readability and body-text fail.
const noArticleShortBodyHTML = `<html><body><div style="display:none">short</div></body></html>`

func TestFetchPage_ReadabilityUsable(t *testing.T) {
	const url = "https://example.com/article"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(readableArticleHTML),
	}, func(context.Context, string) (string, bool) {
		t.Fatal("browser fallback should not be invoked")
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	// go-readability derives the title from page <title>/metadata, not the
	// first <h1>, so the expected title is the page's actual <title> text.
	if !strings.HasPrefix(got, "# Ignored") {
		t.Errorf("fetchPage() = %q; want it to start with the page's <title>", got)
	}
	if !strings.Contains(got, "Source: "+url) {
		t.Errorf("fetchPage() = %q; want it to contain the source line", got)
	}
	if !strings.Contains(got, "genuinely readable article content") {
		t.Errorf("fetchPage() = %q; want it to contain the article body", got)
	}
}

func TestFetchPage_ShortReadabilityTriggersBrowser(t *testing.T) {
	const url = "https://example.com/short-article"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(shortArticleHTML),
	}, func(ctx context.Context, u string) (string, bool) {
		if u != url {
			t.Errorf("browser() called with %q; want %q", u, url)
		}
		return "# Browser Rendered\n\nBrowser-fetched content.", true
	})

	got := fetchPage(context.Background(), f, url)
	if got != "# Browser Rendered\n\nBrowser-fetched content." {
		t.Errorf("fetchPage() = %q; want the browser fallback result", got)
	}
}

func TestFetchPage_ShortReadabilityBrowserAlsoFails(t *testing.T) {
	const url = "https://example.com/short-article-2"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(shortArticleHTML),
	}, func(context.Context, string) (string, bool) {
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	want := "# \n\nSource: " + url + "\n\nTinyToo short."
	if got != want {
		t.Errorf("fetchPage() = %q; want the short readability result anyway: %q", got, want)
	}
}

func TestFetchPage_NoArticleLongBodyFallsBackToBodyText(t *testing.T) {
	const url = "https://example.com/no-article"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(noArticleButLongBodyHTML),
	}, func(context.Context, string) (string, bool) {
		t.Fatal("browser fallback should not be invoked when body text is usable")
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	if !strings.HasPrefix(got, "# "+url) {
		t.Errorf("fetchPage() = %q; want it to start with \"# %s\"", got, url)
	}
	if !strings.Contains(got, "long stretch of plain body text") {
		t.Errorf("fetchPage() = %q; want it to contain the body text", got)
	}
}

func TestFetchPage_NoArticleShortBodyTriggersBrowser(t *testing.T) {
	const url = "https://example.com/no-article-short"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(noArticleShortBodyHTML),
	}, func(ctx context.Context, u string) (string, bool) {
		return "# Browser Rendered\n\nRendered content.", true
	})

	got := fetchPage(context.Background(), f, url)
	if got != "# Browser Rendered\n\nRendered content." {
		t.Errorf("fetchPage() = %q; want the browser fallback result", got)
	}
}

func TestFetchPage_NoExtractionAtAll(t *testing.T) {
	const url = "https://example.com/no-article-short-2"
	f := stubResponses(t, map[string]*http.Response{
		url: htmlResponse(noArticleShortBodyHTML),
	}, func(context.Context, string) (string, bool) {
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	want := "# " + url + "\n\nCould not extract readable content from this page."
	if got != want {
		t.Errorf("fetchPage() = %q; want %q", got, want)
	}
}

func TestFetchPage_TransportErrorDoesNotInvokeBrowser(t *testing.T) {
	const url = "https://example.com/transport-error"
	f := fetcher{
		do: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		},
		browser: func(context.Context, string) (string, bool) {
			t.Fatal("browser fallback should not be invoked on transport error")
			return "", false
		},
	}

	got := fetchPage(context.Background(), f, url)
	if !strings.HasPrefix(got, "# Error fetching "+url) || !strings.Contains(got, "connection refused") {
		t.Errorf("fetchPage() = %q; want an error message with the transport error", got)
	}
}

func TestFetchPage_Non2xxDoesNotInvokeBrowser(t *testing.T) {
	const url = "https://example.com/not-found"
	f := fetcher{
		do: func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
		browser: func(context.Context, string) (string, bool) {
			t.Fatal("browser fallback should not be invoked on non-2xx")
			return "", false
		},
	}

	got := fetchPage(context.Background(), f, url)
	want := "# Error fetching " + url + "\n\nHTTP 404"
	if got != want {
		t.Errorf("fetchPage() = %q; want %q", got, want)
	}
}

func TestFetchPage_RedditUrlRoutesThroughRedditAdapter(t *testing.T) {
	const url = "https://reddit.com/r/golang/comments/1vxc255/small_projects/"

	t.Run("success_path", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		t.Setenv(redditClientIDEnv, "")
		t.Setenv(redditClientSecretEnv, "")
		t.Cleanup(func() { redditTokens.reset() })

		body := readTestdataFile(t, "reddit-thread.rss")
		wantURL, err := redditRSSURL(url)
		if err != nil {
			t.Fatalf("redditRSSURL() error = %v", err)
		}

		f := stubResponses(t, map[string]*http.Response{
			wantURL: htmlResponse(body),
		}, func(context.Context, string) (string, bool) {
			t.Fatal("browser fallback should not be invoked for a handled Reddit fetch")
			return "", false
		})
		// Without an adapters slice, no adapter matches and this URL would
		// wrongly take the generic cascade instead of the Reddit adapter's path.
		f.adapters = defaultAdapters()

		got := fetchPage(context.Background(), f, url)
		if !strings.HasPrefix(got, "# Small Projects") {
			t.Errorf("fetchPage() = %q; want it to start with the post title", got)
		}
		if !strings.Contains(got, "This is the weekly thread for Small Projects.") {
			t.Errorf("fetchPage() = %q; want the RSS-derived post text", got)
		}
		if !strings.Contains(got, "Pingularity") {
			t.Errorf("fetchPage() = %q; want the RSS-derived comment text", got)
		}
	})

	t.Run("all_tiers_fail_still_never_invokes_browser", func(t *testing.T) {
		stubRedditRSSLimiter(t)
		t.Setenv(redditClientIDEnv, "")
		t.Setenv(redditClientSecretEnv, "")
		t.Cleanup(func() { redditTokens.reset() })

		respond := func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("boom"))}, nil
		}
		f := fetcher{
			do: respond,
			browser: func(context.Context, string) (string, bool) {
				t.Fatal("browser fallback must not be invoked even when every Reddit tier fails")
				return "", false
			},
			adapters: defaultAdapters(),
		}

		got := fetchPage(context.Background(), f, url)
		if !strings.HasPrefix(got, "# Error fetching "+url) {
			t.Errorf("fetchPage() = %q; want it to start with \"# Error fetching %s\"", got, url)
		}
	})
}

// redditLikeHTMLWithComments mimics old.reddit.com structure with self-post
// and comments to test that both survive stripToBodyText, unlike Readability.
const redditLikeHTMLWithComments = `<html><head><title>Ignored</title></head><body>
<nav>site nav — must not appear in output</nav>
<div class="thing"><p>This is the original self-post text, long enough on its own to clear the usable-content threshold for this fallback path.</p></div>
<div class="commentarea">
  <div class="comment"><p>First commenter's opinion, also reasonably long so the combined body text is well past the threshold either way.</p></div>
  <div class="comment"><p>Second commenter disagrees and explains why at some length here.</p></div>
</div>
<footer>site footer — must not appear in output</footer>
</body></html>`

// gzipCompress compresses body for use as a stubbed gzip-encoded response.
func gzipCompress(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("gzip.Write() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip.Close() error = %v", err)
	}
	return buf.Bytes()
}

func TestFetchPage_GzipContentEncodingIsDecoded(t *testing.T) {
	const url = "https://example.com/gzipped"
	f := stubResponses(t, map[string]*http.Response{
		url: {
			StatusCode: 200,
			Header:     http.Header{"Content-Encoding": []string{"gzip"}},
			Body:       io.NopCloser(bytes.NewReader(gzipCompress(t, readableArticleHTML))),
		},
	}, func(context.Context, string) (string, bool) {
		t.Fatal("browser fallback should not be invoked once gzip is decoded")
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	if !strings.Contains(got, "genuinely readable article content") {
		t.Errorf("fetchPage() = %q; want the decoded article body, not compressed bytes", got)
	}
}

// brotliCompress compresses body for use as a stubbed br-encoded response.
func brotliCompress(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("brotli.Write() error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("brotli.Close() error = %v", err)
	}
	return buf.Bytes()
}

func TestFetchPage_BrotliContentEncodingIsDecoded(t *testing.T) {
	const url = "https://example.com/brotli"
	f := stubResponses(t, map[string]*http.Response{
		url: {
			StatusCode: 200,
			Header:     http.Header{"Content-Encoding": []string{"br"}},
			Body:       io.NopCloser(bytes.NewReader(brotliCompress(t, readableArticleHTML))),
		},
	}, func(context.Context, string) (string, bool) {
		t.Fatal("browser fallback should not be invoked once brotli is decoded")
		return "", false
	})

	got := fetchPage(context.Background(), f, url)
	if !strings.Contains(got, "genuinely readable article content") {
		t.Errorf("fetchPage() = %q; want the decoded article body, not compressed bytes", got)
	}
}

func TestFetchPage_UnsupportedContentEncodingFallsBackToBrowser(t *testing.T) {
	const url = "https://example.com/compress"
	f := stubResponses(t, map[string]*http.Response{
		url: {
			StatusCode: 200,
			// "compress" (legacy Unix compress/LZW) has no decoder in this
			// package or the standard library, unlike gzip/deflate/br.
			Header: http.Header{"Content-Encoding": []string{"compress"}},
			Body:   io.NopCloser(strings.NewReader("compressed-bytes-irrelevant")),
		},
	}, func(ctx context.Context, u string) (string, bool) {
		if u != url {
			t.Errorf("browser() called with %q; want %q", u, url)
		}
		return "# Browser Rendered\n\nDecoded via the real browser's network stack.", true
	})

	got := fetchPage(context.Background(), f, url)
	want := "# Browser Rendered\n\nDecoded via the real browser's network stack."
	if got != want {
		t.Errorf("fetchPage() = %q; want %q", got, want)
	}
}

func TestFetchPage_ChallengePageIsNotReturnedAsContent(t *testing.T) {
	blockPageBody, err := os.ReadFile("testdata/reddit-block-page.html")
	if err != nil {
		t.Fatalf("os.ReadFile(testdata/reddit-block-page.html) error = %v", err)
	}

	t.Run("static_wall_and_rendered_wall_both_rejected", func(t *testing.T) {
		// A non-Reddit URL is used deliberately: the wall must be rejected by
		// the generic cascade itself, not by any Reddit-specific routing.
		const url = "https://example.com/walled"
		f := stubResponses(t, map[string]*http.Response{
			url: htmlResponse(string(blockPageBody)),
		}, func(context.Context, string) (string, bool) {
			return string(blockPageBody), true
		})

		got := fetchPage(context.Background(), f, url)
		if !strings.HasPrefix(got, "# Error fetching "+url) {
			t.Errorf("fetchPage() = %q; want it to start with \"# Error fetching %s\"", got, url)
		}
		if !strings.Contains(got, "blocked:") {
			t.Errorf("fetchPage() = %q; want it to contain \"blocked:\"", got)
		}
		if strings.Contains(strings.ToLower(got), "blocked by network security") {
			t.Errorf("fetchPage() = %q; want it to NOT contain the block page's marker text", got)
		}
	})

	t.Run("browser_fallback_passes_through_genuine_content", func(t *testing.T) {
		const url = "https://example.com/walled-but-browser-clears-it"
		f := stubResponses(t, map[string]*http.Response{
			url: htmlResponse(string(blockPageBody)),
		}, func(context.Context, string) (string, bool) {
			return "# Browser Rendered\n\nGenuine article text the browser tier recovered.", true
		})

		got := fetchPage(context.Background(), f, url)
		want := "# Browser Rendered\n\nGenuine article text the browser tier recovered."
		if got != want {
			t.Errorf("fetchPage() = %q; want %q", got, want)
		}
	})
}
