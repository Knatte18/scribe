// main_test.go exercises runAll's join-and-order behavior with a stubbed fetcher, no network, no
// subprocess spawn.

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRunAll_JoinsInInputOrder(t *testing.T) {
	urls := []string{
		"https://example.com/one",
		"https://example.com/two",
		"https://example.com/three",
	}
	f := fetcher{
		do: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("<html><body><article><h1>" + req.URL.String() + "</h1></article></body></html>")),
			}, nil
		},
		browser: func(context.Context, string) (string, bool) { return "", false },
	}

	got := runAll(context.Background(), f, urls)
	parts := strings.Split(got, resultJoiner)
	if len(parts) != len(urls) {
		t.Fatalf("runAll() produced %d segments; want %d", len(parts), len(urls))
	}
	for i, u := range urls {
		if !strings.Contains(parts[i], u) {
			t.Errorf("segment %d = %q; want it to contain %q", i, parts[i], u)
		}
	}
}

func TestRunAll_PerURLFailureDoesNotDropSiblings(t *testing.T) {
	const goodURL = "https://example.com/good"
	const badURL = "https://example.com/bad"
	urls := []string{goodURL, badURL, goodURL + "-2"}

	f := fetcher{
		do: func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == badURL {
				return nil, errors.New("connection refused")
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("<html><body><article><h1>" + req.URL.String() + "</h1></article></body></html>")),
			}, nil
		},
		browser: func(context.Context, string) (string, bool) { return "", false },
	}

	got := runAll(context.Background(), f, urls)
	parts := strings.Split(got, resultJoiner)
	if len(parts) != len(urls) {
		t.Fatalf("runAll() produced %d segments; want %d (a failing URL must not drop siblings)", len(parts), len(urls))
	}
	if !strings.HasPrefix(parts[1], "# Error fetching "+badURL) {
		t.Errorf("segment 1 = %q; want the inline error form for the bad URL", parts[1])
	}
	if !strings.Contains(parts[0], goodURL) || !strings.Contains(parts[2], goodURL+"-2") {
		t.Errorf("runAll() = %q; want both good URLs' segments present", got)
	}
}
