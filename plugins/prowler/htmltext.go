// htmltext.go normalizes HTML fragments and full documents down to plain, whitespace-tidy text.
// It backs the "readability failed but the body still has usable text" fallback step of the fetch
// cascade.

package main

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Whitespace-normalization patterns applied by htmlToText.
var (
	horizontalWhitespaceRun = regexp.MustCompile(`[ \t]+`)
	leadingLineWhitespace   = regexp.MustCompile(`\n[ \t]+`)
	excessBlankLines        = regexp.MustCompile(`\n{3,}`)
)

// htmlToText extracts visible text from an HTML fragment and normalizes
// whitespace, removing script/style/noscript elements first.
func htmlToText(fragment string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + fragment + "</div>"))
	if err != nil {
		return ""
	}
	doc.Find("script, style, noscript").Remove()

	text := doc.Find("div").First().Text()
	return normalizeWhitespace(text)
}

// stripToBodyText extracts body text from a full HTML document after removing
// scripts, styles, and page chrome (nav/header/footer).
func stripToBodyText(fullHTML string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fullHTML))
	if err != nil {
		return ""
	}
	doc.Find("script, style, noscript, nav, header, footer").Remove()

	body := doc.Find("body")
	if body.Length() == 0 {
		return ""
	}

	bodyHTML, err := body.Html()
	if err != nil {
		return ""
	}
	return htmlToText(bodyHTML)
}

// normalizeWhitespace collapses noisy HTML whitespace patterns into compact
// readable plain text.
func normalizeWhitespace(text string) string {
	text = horizontalWhitespaceRun.ReplaceAllString(text, " ")
	text = leadingLineWhitespace.ReplaceAllString(text, "\n")
	text = excessBlankLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}
