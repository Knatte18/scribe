// blockdetect.go declares the shared bot-wall/challenge-page detector consumed by the generic
// fetch cascade (fetch.go) and, in later batches, by the Reddit tiers. It exists because the
// cascade's only prior success criterion was "text length", which challenge and block pages
// comfortably clear, so prowler could silently return a wall as if it were real content.

package main

import "strings"

// blockSignature is one recognizable marker of a bot wall, network-security
// block, or challenge interstitial. marker is matched as a substring of the
// already-lowercased candidate text.
type blockSignature struct {
	// name is a short human-readable reason surfaced to the caller, such as
	// "bot challenge" or "network-security block".
	name string
	// marker is an already-lowercased substring that, if present, identifies
	// this signature.
	marker string
}

// blockSignatures is the table of known bot-wall/challenge/block-page
// markers. Order matters only in that the first match wins; markers are
// chosen to be unlikely to appear in genuine article or forum content.
var blockSignatures = []blockSignature{
	{name: "network-security block", marker: "blocked by network security"},
	{name: "bot challenge", marker: "prove your humanity"},
	{name: "bot challenge", marker: "complete the challenge below"},
	{name: "bot challenge", marker: "checking your browser before accessing"},
	{name: "bot challenge", marker: "verifying you are human"},
	{name: "bot challenge", marker: "enable javascript and cookies to continue"},
	{name: "bot challenge", marker: "attention required! | cloudflare"},
	{name: "bot challenge", marker: "verifying your browser"},
	// Discovered from the captured plugins/prowler/testdata/reddit-www-interstitial.html:
	// the interstitial's "Prove your humanity" text only appears after its
	// JavaScript runs (see _mill/discussion.md's Gotchas), so none of the
	// markers above are present in the static bytes curl captures. The
	// auto-submitting challenge form's hidden js_challenge field is,
	// however, present statically and does not appear in genuine article or
	// Reddit-post content.
	{name: "bot challenge", marker: `name="js_challenge"`},
}

// looksLikeBlockPage reports whether text is a bot wall, network-security
// block, or challenge interstitial rather than genuine content, by matching
// it against blockSignatures. text is lowercased once before matching, so
// callers pass raw HTML, extracted plain text, or a JSON error body without
// needing to normalize case themselves. When a signature matches, reason is
// that signature's name and blocked is true; otherwise reason is "" and
// blocked is false.
func looksLikeBlockPage(text string) (reason string, blocked bool) {
	lowered := strings.ToLower(text)
	for _, sig := range blockSignatures {
		if strings.Contains(lowered, sig.marker) {
			return sig.name, true
		}
	}
	return "", false
}
