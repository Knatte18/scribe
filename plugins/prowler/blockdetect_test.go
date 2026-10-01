// blockdetect_test.go proves looksLikeBlockPage flags the real captured wall fixtures and does not
// flag genuine content, using os.ReadFile against plugins/prowler/testdata plus the in-package HTML
// constants fetch_test.go already declares for Reddit/article content.
// No network call, no process spawn.

package main

import (
	"os"
	"testing"
)

func TestLooksLikeBlockPage(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		wantBlocked bool
	}{
		{
			name:        "reddit_network_security_block_page",
			text:        readTestdataFile(t, "reddit-block-page.html"),
			wantBlocked: true,
		},
		{
			name:        "reddit_www_bot_challenge_interstitial",
			text:        readTestdataFile(t, "reddit-www-interstitial.html"),
			wantBlocked: true,
		},
		{
			name:        "good_article_never_flagged",
			text:        readTestdataFile(t, "good-article.html"),
			wantBlocked: false,
		},
		{
			name:        "reddit_like_html_with_comments_never_flagged",
			text:        redditLikeHTMLWithComments,
			wantBlocked: false,
		},
		{
			name:        "readable_article_html_never_flagged",
			text:        readableArticleHTML,
			wantBlocked: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, blocked := looksLikeBlockPage(tt.text)
			if blocked != tt.wantBlocked {
				t.Errorf("looksLikeBlockPage(%s) blocked = %v, reason = %q; want blocked %v", tt.name, blocked, reason, tt.wantBlocked)
			}
			if blocked && reason == "" {
				t.Errorf("looksLikeBlockPage(%s) blocked = true but reason is empty; want a non-empty reason", tt.name)
			}
			if !blocked && reason != "" {
				t.Errorf("looksLikeBlockPage(%s) blocked = false but reason = %q; want empty reason", tt.name, reason)
			}
		})
	}
}

// readTestdataFile reads a fixture from plugins/prowler/testdata, failing the test on error.
func readTestdataFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", name, err)
	}
	return string(data)
}
