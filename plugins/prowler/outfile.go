// outfile.go writes each fetch's extracted markdown to a uniquely-named file under the scratch
// output directory,
// and derives a human-readable slug for that filename from the fetched URL so a directory listing
// stays legible.

package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// nonAlnumRun matches runs of non-alphanumeric characters for slug creation.
var nonAlnumRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugForURL derives a short, descriptive, filesystem-safe slug from raw for
// directory listings. Uniqueness comes from os.CreateTemp's random suffix.
func slugForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "page"
	}

	parts := []string{parsed.Host}
	for _, seg := range strings.Split(parsed.Path, "/") {
		if seg != "" {
			parts = append(parts, seg)
			break
		}
	}
	joined := strings.ToLower(strings.Join(parts, "-"))

	slug := nonAlnumRun.ReplaceAllString(joined, "-")
	slug = strings.Trim(slug, "-")

	const maxSlugLen = 40
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}

	if slug == "" {
		return "page"
	}
	return slug
}

// writeOutput creates a uniquely-named markdown file under .scratch/, writes
// content into it, and returns the file's absolute path.
func writeOutput(firstURL, content string) (string, error) {
	const scratchDir = ".scratch"
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		return "", err
	}

	pattern := "prowler-" + slugForURL(firstURL) + "-*.md"
	f, err := os.CreateTemp(scratchDir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	return filepath.Abs(name)
}
