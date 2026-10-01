// chrome.go locates a Chrome/Chromium executable on the host so the browser fallback (browser.go)
// knows what binary to launch.
// Discovery never shells out — it only checks the filesystem, so it stays fast and side-effect free
// even when no browser fallback ends up being used.

package main

import "os"

// chromeCandidates lists well-known Chrome install locations, in order after
// CHROME_PATH. The order mirrors weblens' candidate list for consistency.
var chromeCandidates = []string{
	"C:/Program Files/Google/Chrome/Application/chrome.exe",
	"C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/usr/bin/google-chrome",
	"/usr/bin/chromium-browser",
}

// findChromeExecutable locates a Chrome/Chromium binary to drive the
// headless-browser fallback, checking CHROME_PATH first then well-known
// install locations. Returns empty string if none exist.
func findChromeExecutable() string {
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		if pathExists(envPath) {
			return envPath
		}
	}

	for _, candidate := range chromeCandidates {
		if pathExists(candidate) {
			return candidate
		}
	}

	return ""
}

// pathExists reports whether path names a file or directory that exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
