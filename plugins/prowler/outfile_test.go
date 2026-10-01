// outfile_test.go tests slug derivation and unique output-file creation.
// All cases run against in-memory strings or a temp directory — no network, no subprocess, no git.

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestSlugForURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"host_and_path", "https://www.example.com/some/Path", "www-example-com-some"},
		{"non_alnum_collapse", "https://example.com/a!!b--c", "example-com-a-b-c"},
		{"host_only_no_path", "https://example.com", "example-com"},
		{"truncated_to_40_chars", "https://" + strings.Repeat("a", 60) + ".com/x", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"empty_input", "", "page"},
		{"unparseable_url", "http://[::1]:namedport", "page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugForURL(tt.raw)
			if got != tt.want {
				t.Errorf("slugForURL(%q) = %q; want %q", tt.raw, got, tt.want)
			}
			if len(got) > 40 {
				t.Errorf("slugForURL(%q) length = %d; want <= 40", tt.raw, len(got))
			}
		})
	}
}

func TestWriteOutput(t *testing.T) {
	t.Chdir(t.TempDir())

	const concurrency = 20
	paths := make([]string, concurrency)
	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := writeOutput("https://example.com/thing", "hello world")
			if err != nil {
				t.Errorf("writeOutput() error = %v", err)
				return
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()

	// Every concurrent call must produce a distinct absolute path; a
	// collision would mean two callers silently overwrote each other's
	// output, which os.CreateTemp's random suffix exists to prevent.
	seen := make(map[string]bool, concurrency)
	namePattern := regexp.MustCompile(`^prowler-example-com-thing-.+\.md$`)
	for _, p := range paths {
		if p == "" {
			continue
		}
		if seen[p] {
			t.Errorf("writeOutput() produced duplicate path %q", p)
		}
		seen[p] = true

		if !filepath.IsAbs(p) {
			t.Errorf("writeOutput() path %q is not absolute", p)
		}

		base := filepath.Base(p)
		if !namePattern.MatchString(base) {
			t.Errorf("writeOutput() basename %q does not match %s", base, namePattern)
		}

		content, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", p, err)
		}
		if string(content) != "hello world" {
			t.Errorf("file %q content = %q; want %q", p, content, "hello world")
		}
	}
}
