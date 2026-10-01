// chrome_test.go verifies Chrome-discovery ordering using fake executables created under
// t.TempDir().
// No shelling out, no real Chrome required.

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindChromeExecutable_ChromePathWinsWhenSet(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome-fake")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("CHROME_PATH", fake)

	got := findChromeExecutable()
	if got != fake {
		t.Errorf("findChromeExecutable() = %q; want %q", got, fake)
	}
}

func TestFindChromeExecutable_CandidateListOrdering(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHROME_PATH", "")

	// Substitute the candidate list with fake paths under our temp dir so
	// the test never depends on what is actually installed on the machine
	// running it.
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("fake"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", p, err)
		}
	}

	originalCandidates := chromeCandidates
	t.Cleanup(func() { chromeCandidates = originalCandidates })
	chromeCandidates = []string{first, second}

	got := findChromeExecutable()
	if got != first {
		t.Errorf("findChromeExecutable() = %q; want first candidate %q", got, first)
	}
}

func TestFindChromeExecutable_NoneExistReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHROME_PATH", filepath.Join(dir, "does-not-exist"))

	originalCandidates := chromeCandidates
	t.Cleanup(func() { chromeCandidates = originalCandidates })
	chromeCandidates = []string{
		filepath.Join(dir, "also-missing-1"),
		filepath.Join(dir, "also-missing-2"),
	}

	got := findChromeExecutable()
	if got != "" {
		t.Errorf("findChromeExecutable() = %q; want \"\"", got)
	}
}

func TestFindChromeExecutable_ChromePathFallsThroughWhenMissing(t *testing.T) {
	dir := t.TempDir()
	fallback := filepath.Join(dir, "fallback-chrome")
	if err := os.WriteFile(fallback, []byte("fake"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("CHROME_PATH", filepath.Join(dir, "does-not-exist"))

	originalCandidates := chromeCandidates
	t.Cleanup(func() { chromeCandidates = originalCandidates })
	chromeCandidates = []string{fallback}

	got := findChromeExecutable()
	if got != fallback {
		t.Errorf("findChromeExecutable() = %q; want %q", got, fallback)
	}
}
