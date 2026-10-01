// main.go wires the production fetcher and is the binary's entrypoint: it parses the URL arguments,
// runs the fetch cascade for each, joins the results, writes them to a scratch output file, and
// prints that file's path — the single line of stdout the invoking skill wrapper captures.

package main

import (
	"context"
	"fmt"
	"os"
)

// resultJoiner separates each URL's fetched content in the combined output.
const resultJoiner = "\n\n---\n\n"

// newFetcher wires real implementations: httpClient.Do, fetchWithBrowser,
// and defaultAdapters(). It is the only place production code constructs a
// fetcher.
func newFetcher() fetcher {
	return fetcher{
		do:       httpClient.Do,
		browser:  fetchWithBrowser,
		adapters: defaultAdapters(),
	}
}

// runAll fetches every URL via f and joins results with resultJoiner,
// preserving input order. Per-URL failures are captured inline, so one bad
// URL never drops siblings' results.
func runAll(ctx context.Context, f fetcher, urls []string) string {
	results := make([]string, len(urls))

	done := make(chan struct{})
	for i, u := range urls {
		go func(i int, u string) {
			results[i] = fetchPage(ctx, f, u)
			done <- struct{}{}
		}(i, u)
	}
	for range urls {
		<-done
	}

	joined := ""
	for i, r := range results {
		if i > 0 {
			joined += resultJoiner
		}
		joined += r
	}
	return joined
}

// main is the binary's entrypoint. Reads URLs from command line, runs fetch
// cascade, and writes combined markdown to a scratch file. Prints the file's
// absolute path to stdout.
func main() {
	urls := os.Args[1:]
	if len(urls) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: prowler <url> [url2] [url3]...")
		os.Exit(1)
	}

	joined := runAll(context.Background(), newFetcher(), urls)

	path, err := writeOutput(urls[0], joined)
	if err != nil {
		fmt.Fprintln(os.Stderr, "prowler: failed to write output file: "+err.Error())
		os.Exit(1)
	}

	fmt.Println(path)
}
