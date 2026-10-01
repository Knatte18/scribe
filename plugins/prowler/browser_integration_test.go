//go:build integration

// browser_integration_test.go drives the real headless-Chrome fallback
// against a known-readable live page. It requires network access and a
// local Chrome (via CHROME_PATH or findChromeExecutable's discovery), so it
// is excluded from the fast unit run and must be invoked explicitly with
// `go test -tags integration ./...`. The chromedp library spawns the browser
// process internally; this file itself never shells out or spawns a
// subprocess directly.

package main

import (
	"context"
	"strings"
	"testing"
)

func TestFetchWithBrowser_Integration(t *testing.T) {
	if findChromeExecutable() == "" {
		t.Skip("no Chrome executable found (set CHROME_PATH or install Chrome/Chromium)")
	}

	const url = "https://example.com/"
	text, ok := fetchWithBrowser(context.Background(), url)
	if !ok {
		t.Fatalf("fetchWithBrowser(%q) ok = false; want true", url)
	}
	if !strings.Contains(text, "Example Domain") {
		t.Errorf("fetchWithBrowser(%q) = %q; want it to contain the page's known content", url, text)
	}
}
