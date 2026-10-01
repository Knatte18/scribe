// browser.go drives a headless Chrome instance via chromedp as the fetch cascade's last-resort
// fallback, for pages whose content only exists after client-side JavaScript runs (which no static
// HTTP fetch can ever see).

package main

import (
	"context"
	"fmt"
	nurl "net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	readability "github.com/go-shiori/go-readability"
)

// browserOverallTimeout bounds the whole headless-browser fallback attempt
// (allocator startup, navigation, render settle, and HTML capture) so one
// hung page can never stall a multi-URL run indefinitely.
const browserOverallTimeout = 60 * time.Second

// browserNavigationTimeout bounds Navigate specifically, distinct from the
// overall timeout, so a slow-loading page fails fast without waiting to
// exhaust the entire overall budget on navigation alone.
const browserNavigationTimeout = 30 * time.Second

// renderSettleDelay is a fixed pause inserted after the page reports
// "ready" (body present) and before capturing HTML. chromedp has no
// built-in equivalent to Puppeteer's networkidle0 wait condition — Navigate
// itself only waits for the browser's `load` event, which fires before
// client-side JS has had a chance to render dynamic content. This sleep is
// a deliberate, if imperfect, parity substitute: exactly the JS-rendered
// content this fallback exists to capture needs a moment to run.
const renderSettleDelay = 2 * time.Second

// fetchWithBrowser renders url in headless Chrome and extracts readable
// content, for pages a static fetch cannot see. It reports ok=false when no
// usable content could be produced, including when no Chrome is available.
func fetchWithBrowser(ctx context.Context, url string) (out string, ok bool) {
	chromePath := findChromeExecutable()
	if chromePath == "" {
		fmt.Fprintln(os.Stderr, "prowler: Chrome not found, skipping browser fallback")
		return "", false
	}
	fmt.Fprintln(os.Stderr, "prowler: falling back to headless Chrome for "+url)

	allocOpts := append(
		append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...),
		chromedp.ExecPath(chromePath),
		chromedp.Flag("headless", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	overallCtx, overallCancel := context.WithTimeout(ctx, browserOverallTimeout)
	defer overallCancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(overallCtx, allocOpts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	navCtx, navCancel := context.WithTimeout(browserCtx, browserNavigationTimeout)
	defer navCancel()

	var renderedHTML string
	err := chromedp.Run(navCtx,
		chromedp.Navigate(url),
		// Navigate returns at the browser's load event, before client-side
		// JS has necessarily finished rendering — wait for the body to
		// exist, then give in-flight rendering a moment to settle before
		// capturing HTML.
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(renderSettleDelay),
		chromedp.OuterHTML("html", &renderedHTML, chromedp.ByQuery),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prowler: headless Chrome fetch failed for "+url+": "+err.Error())
		return "", false
	}

	// A parse failure here just means relative links in the rendered content
	// won't be resolved to absolute URLs; extraction still proceeds with a
	// nil pageURL rather than aborting the whole fallback over it.
	parsedURL, _ := nurl.Parse(url)
	article, readabilityErr := readability.FromReader(strings.NewReader(renderedHTML), parsedURL)
	if readabilityErr == nil && article.TextContent != "" {
		return "# " + article.Title + "\n\nSource: " + url + "\n\n" + article.TextContent, true
	}

	if bodyText := stripToBodyText(renderedHTML); len(bodyText) > minUsableTextLen {
		return "# " + url + "\n\n" + bodyText, true
	}

	return "", false
}
