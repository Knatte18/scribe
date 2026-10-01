// htmltext_test.go exercises the HTML-to-text normalization helpers with fixed in-memory HTML
// fixtures.
// No network, no subprocess.

package main

import (
	"strings"
	"testing"
)

func TestHtmlToText(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
		want     string
	}{
		{
			name:     "strips_script_style_noscript",
			fragment: `<p>Hello</p><script>evil()</script><style>.x{}</style><noscript>no js</noscript><p>World</p>`,
			want:     "HelloWorld",
		},
		{
			name:     "collapses_horizontal_whitespace",
			fragment: "<p>Hello    \t   World</p>",
			want:     "Hello World",
		},
		{
			name:     "collapses_leading_line_whitespace",
			fragment: "<p>Hello</p>\n    <p>World</p>",
			want:     "Hello\nWorld",
		},
		{
			name:     "collapses_blank_lines",
			fragment: "<p>Hello</p>\n\n\n\n<p>World</p>",
			want:     "Hello\n\nWorld",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlToText(tt.fragment)
			if got != tt.want {
				t.Errorf("htmlToText(%q) = %q; want %q", tt.fragment, got, tt.want)
			}
		})
	}
}

func TestStripToBodyText(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "strips_nav_header_footer_and_scripts",
			html: `<html><head><style>.x{}</style></head><body>` +
				`<header>Site Header</header><nav>Menu</nav>` +
				`<p>Main content here.</p>` +
				`<script>track()</script>` +
				`<footer>Site Footer</footer>` +
				`</body></html>`,
			want: "Main content here.",
		},
		{
			name: "no_body_returns_empty",
			html: `<html><head><title>t</title></head></html>`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripToBodyText(tt.html)
			if got != tt.want {
				t.Errorf("stripToBodyText(...) = %q; want %q", got, tt.want)
			}
			if strings.Contains(got, "Header") || strings.Contains(got, "Footer") || strings.Contains(got, "Menu") {
				t.Errorf("stripToBodyText(...) leaked chrome content: %q", got)
			}
		})
	}
}
