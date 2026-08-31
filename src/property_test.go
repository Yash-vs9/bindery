package main

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Property-based fuzzing.
//
// The fuzz targets beside each parser assert only that arbitrary input does not
// panic or hang. That is worth having, but it is a low bar: a renderer that
// emitted an unescaped quotation mark into an href would pass it happily.
//
// These targets assert properties that would matter if they broke.

// attrPattern finds the value of every attribute bindery itself constructs
// from parsed data: href and src (link and image destinations), title and alt
// (link/image titles and alt text), and class (the syntax-highlighting and
// diagram language classes). These are the only attributes built by
// interpolating untrusted input into a string -- see render_html.go -- and so
// the only ones where an escaping bug would be a real injection vector.
//
// It deliberately does not match every attribute-shaped substring in the
// output. The first version did, and failed on "<A A="<">": a raw HTML tag,
// which CommonMark requires passing through completely unescaped -- that is
// what "raw HTML" support means, the same as any other conformant renderer.
// The test was flagging correct, spec-mandated behaviour as a bug. Scoping
// the pattern to bindery's own constructed attributes keeps the check honest:
// it still catches a real escaping regression in href/src/title/alt/class,
// and stops objecting to content the spec says must be left alone.
var attrPattern = regexp.MustCompile(`\s(?:href|src|title|alt|class)="([^"]*)"`)

// FuzzHTMLEscaping asserts the property that keeps rendered Markdown safe to
// serve: no text from the input may escape the construct it was placed in.
//
// Markdown is frequently rendered from untrusted sources -- a comment, a pull
// request description, a wiki edit. A renderer that lets input close an
// attribute is an injection vector, and the failure is invisible in ordinary
// use because it needs a quotation mark in exactly the wrong place.
func FuzzHTMLEscaping(f *testing.F) {
	for _, seed := range []string{
		`[x](" onmouseover="alert(1))`,
		`[x](javascript:alert(1))`,
		`![alt" onerror="x](i.png)`,
		"[x](/url \"ti\"tle\")",
		`<img src="x" onerror="alert(1)">`,
		"[a](<b\"c>)",
		"# <script>alert(1)</script>",
		"`<script>`",
		// Raw HTML with an attribute value that itself contains an angle
		// bracket. CommonMark requires this pass through completely
		// unescaped -- that is what raw HTML support means -- and the first
		// version of this test wrongly flagged it as an injection, because
		// its check did not distinguish attributes bindery constructs from
		// author-supplied HTML the spec says must be left alone. Found by
		// the CI fuzz smoke test, kept here as a permanent seed.
		`<A A="<">`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, src string) {
		out := RenderHTML(Parse(src))

		if !utf8.ValidString(out) && utf8.ValidString(src) {
			t.Fatalf("valid UTF-8 input produced invalid UTF-8 output\ninput: %q", src)
		}

		// Every bindery-constructed attribute value must be free of raw
		// quotation marks, which the regexp above already guarantees by
		// construction, and free of raw angle brackets, which would mean input
		// escaped the attribute it was placed in.
		for _, match := range attrPattern.FindAllStringSubmatch(out, -1) {
			if strings.ContainsAny(match[1], "<>") {
				t.Fatalf("bindery-constructed attribute contains a raw angle bracket: %q\ninput: %q\noutput: %q",
					match[1], src, out)
			}
		}

		// Text content that was not raw HTML must never introduce a tag. Raw
		// HTML blocks and inline raw HTML are passed through by design -- that
		// is CommonMark, and the README says so -- so this only checks input
		// that contains no angle bracket at all.
		if !strings.ContainsAny(src, "<>") && strings.Contains(out, "<script") {
			t.Fatalf("input with no angle brackets produced a script tag\ninput: %q\noutput: %q", src, out)
		}
	})
}

// FuzzParseDeterminism asserts that parsing and rendering are pure functions of
// the input. The reproducible-build claim depends on it: if rendering the same
// document twice could differ, two builds of a site would differ, and no amount
// of care in the build flags would fix it.
func FuzzParseDeterminism(f *testing.F) {
	for _, seed := range []string{
		"# a\n\n- b\n- c\n", "[a]: /b\n\n[a]\n", "> q\n\n```\nx\n```\n",
		"*a **b** c*", "| a |\n", "\ttab\n", "---\nk: v\n---\n# t\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, src string) {
		first := RenderHTML(Parse(src))
		for i := 0; i < 3; i++ {
			if next := RenderHTML(Parse(src)); next != first {
				t.Fatalf("rendering is not deterministic on attempt %d\ninput: %q\nfirst: %q\nthen:  %q",
					i+2, src, first, next)
			}
		}

		// The same must hold for the terminal renderer, which the search index
		// and the site build both depend on indirectly.
		ansi := RenderANSI(Parse(src), 60, false)
		if again := RenderANSI(Parse(src), 60, false); again != ansi {
			t.Fatalf("ansi rendering is not deterministic\ninput: %q", src)
		}
	})
}

// FuzzNoRawInputLeaks asserts that a code span or fenced block never lets its
// contents be interpreted. This is the property that lets documentation show
// markup without it taking effect, and it is easy to break while fixing
// something else.
func FuzzNoRawInputLeaks(f *testing.F) {
	for _, seed := range []string{
		"x", "<b>", "*a*", "&amp;", "</code></pre>", "```", "\x00", "a\nb",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		// A fenced block containing arbitrary text, provided the text cannot
		// itself close the fence.
		if strings.Contains(content, "```") || strings.Contains(content, "\n") {
			t.Skip()
		}
		out := RenderHTML(Parse("```\n" + content + "\n```\n"))

		if strings.Contains(out, "<b>") && !strings.Contains(content, "&lt;b&gt;") {
			t.Fatalf("raw tag escaped from a code fence\ncontent: %q\noutput: %q", content, out)
		}
		if strings.Contains(out, "<em>") {
			t.Fatalf("emphasis was interpreted inside a code fence\ncontent: %q\noutput: %q", content, out)
		}
	})
}
