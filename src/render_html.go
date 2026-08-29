package main

import (
	"fmt"
	"strconv"
	"strings"
)

// HTML rendering.
//
// The output shape is dictated by the CommonMark specification down to the
// placement of newlines, because the conformance suite compares rendered HTML
// byte for byte. Deviating to taste costs conformance, so this follows the
// reference renderer exactly -- including the void-element spelling "<hr />".
//
// The newline discipline is the part worth explaining. Blocks do not simply
// append "\n" after themselves; they call cr(), which writes a newline only if
// the output is not already at the start of one. That single rule is what makes
// nested structures come out right: a <li> immediately followed by a tight
// paragraph produces "<li>foo", while the same <li> followed by a loose
// paragraph produces "<li>\n<p>foo</p>". Two cases, one rule, no special-casing.

// RenderOptions turns on rendering that goes beyond the specification.
//
// Highlighting is an option rather than the default for one reason: the
// conformance suite compares against reference output, which is plain escaped
// text inside <pre><code>. Colouring code by default would mean either failing
// 29 fenced-code examples or measuring conformance against a renderer nobody
// uses. So "bindery spec" renders with the zero value and the site build turns
// highlighting on, and the reported score stays a claim about the parser.
type RenderOptions struct {
	Highlight  bool
	HeadingIDs bool
	Diagrams   bool
}

// htmlWriter is a string builder that tracks whether it sits at line start.
type htmlWriter struct {
	sb          strings.Builder
	atLineStart bool
	opts        RenderOptions
}

func newHTMLWriter(opts RenderOptions) *htmlWriter {
	return &htmlWriter{atLineStart: true, opts: opts}
}

func (w *htmlWriter) write(s string) {
	if s == "" {
		return
	}
	w.sb.WriteString(s)
	w.atLineStart = s[len(s)-1] == '\n'
}

// cr writes a newline unless one is already there.
func (w *htmlWriter) cr() {
	if !w.atLineStart {
		w.write("\n")
	}
}

func (w *htmlWriter) String() string { return w.sb.String() }

// RenderHTML renders a parsed document as a specification-shaped HTML fragment.
func RenderHTML(d *Document) string {
	return RenderHTMLWith(d, RenderOptions{})
}

// RenderHTMLWith renders a parsed document with the given options.
func RenderHTMLWith(d *Document, opts RenderOptions) string {
	w := newHTMLWriter(opts)
	renderBlock(w, d.Root)
	return w.String()
}

func renderChildren(w *htmlWriter, b *Block) {
	for _, child := range b.Children {
		renderBlock(w, child)
	}
}

func renderBlock(w *htmlWriter, b *Block) {
	switch b.Kind {
	case KindDocument:
		renderChildren(w, b)

	case KindParagraph:
		// A paragraph inside a tight list item contributes its inlines
		// directly, with no <p> wrapper. That is the whole difference between
		// a tight list and a loose one.
		if inTightList(b) {
			renderInlines(w, b.Inlines)
			return
		}
		w.cr()
		w.write("<p>")
		renderInlines(w, b.Inlines)
		w.write("</p>")
		w.cr()

	case KindHeading:
		level := strconv.Itoa(b.Level)
		w.cr()
		if w.opts.HeadingIDs && b.slug != "" {
			w.write("<h" + level + ` id="` + escapeHTML(b.slug) + `">`)
		} else {
			w.write("<h" + level + ">")
		}
		renderInlines(w, b.Inlines)
		w.write("</h" + level + ">")
		w.cr()

	case KindThematicBreak:
		w.cr()
		w.write("<hr />")
		w.cr()

	case KindCodeFenced:
		// A diagram fence becomes a picture. If it does not parse as one it
		// falls through to being rendered as code, which is more useful than an
		// error message where a diagram should be.
		if w.opts.Diagrams && isDiagramLanguage(firstWord(b.Info)) {
			if svg, ok := renderDiagram(b.Text()); ok {
				w.cr()
				w.write(`<figure class="bd-figure">` + svg + "</figure>")
				w.cr()
				return
			}
		}
		w.cr()
		w.write("<pre><code")
		// Only the first word of the info string becomes the language class.
		if lang := firstWord(b.Info); lang != "" {
			w.write(` class="language-` + escapeHTML(unescapeBackslashes(lang)) + `"`)
		}
		w.write(">")
		if lang := firstWord(b.Info); w.opts.Highlight && lang != "" {
			w.write(highlightHTML(b.Text(), unescapeBackslashes(lang)))
		} else {
			w.write(escapeHTML(b.Text()))
		}
		w.write("</code></pre>")
		w.cr()

	case KindCodeIndented:
		w.cr()
		w.write("<pre><code>")
		w.write(escapeHTML(b.Text()))
		w.write("</code></pre>")
		w.cr()

	case KindQuote:
		w.cr()
		w.write("<blockquote>")
		w.cr()
		renderChildren(w, b)
		w.cr()
		w.write("</blockquote>")
		w.cr()

	case KindList:
		w.cr()
		if b.Ordered {
			if b.Start != 1 {
				w.write(`<ol start="` + strconv.Itoa(b.Start) + `">`)
			} else {
				w.write("<ol>")
			}
		} else {
			w.write("<ul>")
		}
		w.cr()
		renderChildren(w, b)
		w.cr()
		if b.Ordered {
			w.write("</ol>")
		} else {
			w.write("</ul>")
		}
		w.cr()

	case KindListItem:
		w.cr()
		w.write("<li>")
		renderChildren(w, b)
		w.write("</li>")
		w.cr()

	case KindHTMLBlock:
		w.cr()
		w.write(b.Text())
		w.cr()
	}
}

// inTightList reports whether b is a paragraph directly inside an item of a
// tight list.
func inTightList(b *Block) bool {
	item := b.parent
	if item == nil || item.Kind != KindListItem {
		return false
	}
	list := item.parent
	return list != nil && list.Kind == KindList && list.Tight
}

func renderInlines(w *htmlWriter, inlines []Inline) {
	for _, in := range inlines {
		switch in.Kind {
		case InlineText:
			w.write(escapeHTML(in.Text))
		case InlineCode:
			w.write("<code>" + escapeHTML(in.Text) + "</code>")
		case InlineEmph:
			w.write("<em>")
			renderInlines(w, in.Children)
			w.write("</em>")
		case InlineStrong:
			w.write("<strong>")
			renderInlines(w, in.Children)
			w.write("</strong>")
		case InlineLink:
			w.write(`<a href="` + escapeURL(in.Dest) + `"`)
			if in.Title != "" {
				w.write(` title="` + escapeHTML(in.Title) + `"`)
			}
			w.write(">")
			renderInlines(w, in.Children)
			w.write("</a>")
		case InlineImage:
			w.write(`<img src="` + escapeURL(in.Dest) + `" alt="` + escapeHTML(plainText(in.Children)) + `"`)
			if in.Title != "" {
				w.write(` title="` + escapeHTML(in.Title) + `"`)
			}
			w.write(" />")
		case InlineRawHTML:
			w.write(in.Text)
		case InlineSoftBreak:
			w.write("\n")
		case InlineHardBreak:
			w.write("<br />")
			w.cr()
		}
	}
}

// plainText flattens inline content to bare text, for image alt attributes.
func plainText(inlines []Inline) string {
	var sb strings.Builder
	var walk func([]Inline)
	walk = func(list []Inline) {
		for _, in := range list {
			switch in.Kind {
			case InlineText, InlineCode:
				sb.WriteString(in.Text)
			case InlineSoftBreak, InlineHardBreak:
				sb.WriteString("\n")
			default:
				walk(in.Children)
			}
		}
	}
	walk(inlines)
	return sb.String()
}

// isDiagramLanguage reports whether an info string asks for a diagram.
func isDiagramLanguage(lang string) bool {
	switch strings.ToLower(lang) {
	case "mermaid", "flowchart", "graph":
		return true
	}
	return false
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// escapeHTML escapes the four characters the specification requires escaping in
// text and attribute content. html.EscapeString is deliberately not used: it
// also escapes the apostrophe as &#39;, which does not match reference output.
func escapeHTML(s string) string {
	if !strings.ContainsAny(s, `&<>"`) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 16)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// hrefSafe lists the bytes that may appear unencoded in a rendered URL. Anything
// else is percent-encoded, which is what the reference implementation does and
// therefore what the conformance suite expects.
const hrefSafe = "-_.+!*'(),%#@?=;:/&$~"

func escapeURL(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '&':
			sb.WriteString("&amp;")
		case c == '\'':
			sb.WriteString("&#x27;")
		case isAlpha(c) || isDigit(c) || strings.IndexByte(hrefSafe, c) >= 0:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}
