package main

import (
	"strconv"
	"strings"
	"unicode"
)

// Headings, slugs and the table of contents.
//
// This replaces github-slugger, which exists because turning "Getting Started
// (v2)" into "getting-started-v2" involves more decisions than it appears:
// which characters survive, what happens to runs of punctuation, and what to do
// when two headings produce the same slug.

// Heading is one entry in a page's table of contents.
type Heading struct {
	Level int
	Text  string
	Slug  string
}

// extractHeadings walks a document, assigning each heading a unique slug and
// returning them in document order.
//
// The slug is stored on the block as well as returned, so that the renderer and
// the table of contents cannot disagree about what an anchor is called. Deriving
// it twice would be one derivation too many.
func extractHeadings(doc *Document) []Heading {
	seen := map[string]int{}
	var headings []Heading

	var walk func(b *Block)
	walk = func(b *Block) {
		if b.Kind == KindHeading {
			text := strings.TrimSpace(plainText(b.Inlines))
			slug := uniqueSlug(slugify(text), seen)
			b.slug = slug
			headings = append(headings, Heading{Level: b.Level, Text: text, Slug: slug})
		}
		for _, child := range b.Children {
			walk(child)
		}
	}
	walk(doc.Root)
	return headings
}

// slugify converts heading text into an anchor.
//
// Letters and digits survive, in any script: unicode.IsLetter accepts Cyrillic
// and CJK as readily as ASCII, so a heading in another language gets a readable
// anchor rather than an empty one. Everything else becomes a separator, runs of
// separators collapse, and leading and trailing ones are dropped.
func slugify(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))
	pendingDash := false

	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingDash && sb.Len() > 0 {
				sb.WriteByte('-')
			}
			pendingDash = false
			sb.WriteRune(r)
		default:
			// Punctuation and whitespace alike become a single separator, but
			// only once something follows -- which is what stops "Hello!" from
			// slugging to "hello-".
			if sb.Len() > 0 {
				pendingDash = true
			}
		}
	}

	if sb.Len() == 0 {
		// A heading of nothing but punctuation still needs an anchor.
		return "section"
	}
	return sb.String()
}

// uniqueSlug appends a counter when a slug has been used before, so that two
// headings called "Usage" get distinct anchors.
func uniqueSlug(slug string, seen map[string]int) string {
	n, used := seen[slug]
	if !used {
		seen[slug] = 1
		return slug
	}
	for {
		n++
		candidate := slug + "-" + strconv.Itoa(n)
		if _, taken := seen[candidate]; !taken {
			seen[slug] = n
			seen[candidate] = 1
			return candidate
		}
	}
}

// TOC returns the headings worth showing in a sidebar.
//
// Level one is the page title, which the sidebar already shows, and anything
// below level three is too fine-grained to navigate by.
func TOC(headings []Heading) []Heading {
	var out []Heading
	for _, h := range headings {
		if h.Level >= 2 && h.Level <= 3 {
			out = append(out, h)
		}
	}
	return out
}
