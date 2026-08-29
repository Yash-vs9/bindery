package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// PDF output.
//
// A PDF is a text format with a binary reputation. It is a sequence of numbered
// objects, a cross-reference table listing the byte offset of each, and a
// trailer pointing at the table. Page content is a small stack language: set a
// font, set a position, show a string. Everything in this file is string
// building and arithmetic.
//
// What makes a dependency-free PDF writer practical is the base-14 fonts. Every
// conforming reader ships Helvetica, Courier and Times, so a document that uses
// them embeds no font programme -- no font parsing, no hinting, no
// rasterisation. The only thing a writer still needs is advance widths, to break
// lines, and those are in pdfmetrics.go.
//
// The honest cost of that choice is in the README: base-14 fonts cover Latin
// text and nothing else, so non-ASCII characters are replaced rather than
// mangled. Rendering CJK or Cyrillic would mean embedding a font, which means
// parsing one.

// Page geometry, in PostScript points (1/72 inch). A4.
const (
	pdfPageWidth  = 595.28
	pdfPageHeight = 841.89
	pdfMargin     = 56.7 // 2cm
	pdfTextWidth  = pdfPageWidth - 2*pdfMargin
)

// pdfFont is one of the base-14 fonts this writer uses.
type pdfFont int

const (
	fontRegular pdfFont = iota
	fontBold
	fontItalic
	fontMono
)

// resource returns the name this font is given in the page's resource
// dictionary.
func (f pdfFont) resource() string {
	return [...]string{"F1", "F2", "F3", "F4"}[f]
}

// baseFont returns the PostScript name of the underlying base-14 font.
func (f pdfFont) baseFont() string {
	return [...]string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Courier"}[f]
}

// width returns the advance width of one byte at the given size.
func (f pdfFont) width(c byte, size float64) float64 {
	if f == fontMono {
		return courierWidth * size / 1000
	}
	if c < asciiFirst || c > 126 {
		c = '?'
	}
	i := c - asciiFirst
	var w int16
	switch f {
	case fontBold:
		w = helveticaBoldWidths[i]
	case fontItalic:
		w = helveticaObliqueWidths[i]
	default:
		w = helveticaWidths[i]
	}
	return float64(w) * size / 1000
}

// measure returns the width of a string, which must already be ASCII.
func (f pdfFont) measure(s string, size float64) float64 {
	total := 0.0
	for i := 0; i < len(s); i++ {
		total += f.width(s[i], size)
	}
	return total
}

// toASCII replaces anything the base-14 fonts cannot show.
//
// This is the visible edge of the no-font-embedding decision. A smart quote
// becomes a straight one and an em dash becomes a hyphen, both of which read
// fine; anything else becomes a question mark, which does not, and the README
// says so.
func toASCII(s string) string {
	if isASCII(s) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 128:
			sb.WriteByte(byte(r))
		case r == '‘' || r == '’':
			sb.WriteByte('\'')
		case r == '“' || r == '”':
			sb.WriteByte('"')
		case r == '–' || r == '—':
			sb.WriteByte('-')
		case r == '…':
			sb.WriteString("...")
		case r == ' ':
			sb.WriteByte(' ')
		default:
			sb.WriteByte('?')
		}
	}
	return sb.String()
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}

// escapePDFString escapes the three characters that are special inside a PDF
// literal string.
func escapePDFString(s string) string {
	if !strings.ContainsAny(s, `()\`) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// pdfWord is one typeset word, carrying the style it was written in.
//
// Wrapping happens over words rather than over runs of styled text, because a
// line break can fall inside a bold phrase and the styles have to survive it.
type pdfWord struct {
	text  string
	font  pdfFont
	size  float64
	link  string
	grey  float64 // 0 is black
	glue  bool    // no space before this word
	width float64
}

// pdfPage is one accumulated page.
type pdfPage struct {
	content strings.Builder
	annots  []string
}

// pdfDoc accumulates pages.
type pdfDoc struct {
	pages []*pdfPage
	y     float64
}

func newPDFDoc() *pdfDoc {
	d := &pdfDoc{}
	d.newPage()
	return d
}

func (d *pdfDoc) page() *pdfPage { return d.pages[len(d.pages)-1] }

func (d *pdfDoc) newPage() {
	d.pages = append(d.pages, &pdfPage{})
	d.y = pdfPageHeight - pdfMargin
}

// ensure starts a new page if the next h points would not fit.
func (d *pdfDoc) ensure(h float64) {
	if d.y-h < pdfMargin {
		d.newPage()
	}
}

// showText emits one positioned string.
func (d *pdfDoc) showText(text string, f pdfFont, size, x, y, grey float64) {
	p := d.page()
	fmt.Fprintf(&p.content, "%.3f %.3f %.3f rg\nBT /%s %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n",
		grey, grey, grey, f.resource(), size, x, y, escapePDFString(text))
}

// rect fills a rectangle, used for rules and code-block backgrounds.
func (d *pdfDoc) rect(x, y, w, h, grey float64) {
	fmt.Fprintf(&d.page().content, "%.3f %.3f %.3f rg\n%.2f %.2f %.2f %.2f re f\n",
		grey, grey, grey, x, y, w, h)
}

// link records a clickable region. PDF links are annotations on the page rather
// than part of the content stream, which is why they are collected separately.
func (d *pdfDoc) link(url string, x, y, w, h float64) {
	p := d.page()
	p.annots = append(p.annots, fmt.Sprintf(
		"<< /Type /Annot /Subtype /Link /Border [0 0 0] /Rect [%.2f %.2f %.2f %.2f] "+
			"/A << /S /URI /URI (%s) >> >>",
		x, y, x+w, y+h, escapePDFString(toASCII(url))))
}

// paragraph typesets words, wrapping to the available width.
func (d *pdfDoc) paragraph(words []pdfWord, indent, leading float64) {
	if len(words) == 0 {
		return
	}
	maxWidth := pdfTextWidth - indent

	line := make([]pdfWord, 0, 16)
	lineWidth := 0.0

	flush := func() {
		if len(line) == 0 {
			return
		}
		d.ensure(leading)
		x := pdfMargin + indent
		baseline := d.y - leading*0.8
		for i, w := range line {
			if !w.glue && i > 0 {
				x += gapWidth(line[i-1], w)
			}
			d.showText(w.text, w.font, w.size, x, baseline, w.grey)
			if w.link != "" {
				d.link(w.link, x, baseline-2, w.width, w.size)
			}
			x += w.width
		}
		d.y -= leading
		line = line[:0]
		lineWidth = 0
	}

	for _, w := range words {
		space := 0.0
		if !w.glue && len(line) > 0 {
			space = gapWidth(line[len(line)-1], w)
		}
		if len(line) > 0 && lineWidth+space+w.width > maxWidth {
			flush()
			w.glue = true // first word on a line never carries a leading space
		}
		if len(line) > 0 {
			lineWidth += space
		}
		line = append(line, w)
		lineWidth += w.width
	}
	flush()
}

// wordBuilder flattens inline nodes into styled words.
//
// It is a struct rather than a recursive function because word spacing is
// stateful: whether a word needs a space in front of it depends on what came
// before, and "before" may be in a different inline node at a different depth.
// A plain recursion loses that at every level, which is how "live reload. One
// binary" first came out as "live reload.One binary".
type wordBuilder struct {
	words   []pdfWord
	size    float64
	pending bool // a space is owed before the next word
}

// text adds a run of literal text in one style.
func (wb *wordBuilder) text(raw string, font pdfFont, grey float64, link string) {
	ascii := toASCII(raw)
	if ascii == "" {
		return
	}
	if startsWithSpace(ascii) {
		wb.pending = true
	}

	fields := strings.Fields(ascii)
	for i, field := range fields {
		// Only the first word of a run can be glued to what precedes it;
		// within a run the fields were separated by whitespace by definition.
		glue := i == 0 && len(wb.words) > 0 && !wb.pending
		wb.words = append(wb.words, pdfWord{
			text:  field,
			font:  font,
			size:  wb.size,
			link:  link,
			grey:  grey,
			glue:  glue,
			width: font.measure(field, wb.size),
		})
		wb.pending = false
	}

	if len(fields) > 0 && endsWithSpace(ascii) {
		wb.pending = true
	}
}

// inlines walks inline nodes, carrying the style down.
func (wb *wordBuilder) inlines(list []Inline, font pdfFont, link string) {
	for _, in := range list {
		switch in.Kind {
		case InlineText:
			wb.text(in.Text, font, 0, link)
		case InlineCode:
			wb.text(in.Text, fontMono, 0.15, link)
		case InlineEmph:
			wb.inlines(in.Children, fontItalic, link)
		case InlineStrong:
			wb.inlines(in.Children, fontBold, link)
		case InlineLink:
			// Only absolute links become clickable. A relative link points at a
			// file that will not exist beside the PDF, and an anchor points into
			// a page structure the PDF does not have -- both are dead links that
			// look alive, which is worse than plain text.
			target := in.Dest
			if !isAbsoluteURL(target) {
				target = ""
			}
			wb.inlines(in.Children, font, target)
		case InlineImage:
			wb.text("[image: "+plainText(in.Children)+"]", fontItalic, 0.45, link)
		case InlineSoftBreak, InlineHardBreak:
			// A line break in the source is a space on the page. Nothing is
			// emitted; the next word simply owes a space.
			wb.pending = true
		case InlineRawHTML:
			// Raw HTML has no meaning on paper.
		}
	}
}

func startsWithSpace(s string) bool {
	return len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t')
}

func endsWithSpace(s string) bool {
	c := s[len(s)-1]
	return c == ' ' || c == '\n' || c == '\t'
}

// gapWidth returns the width of the space between two words.
//
// The space belongs to the text that precedes it, not to the word that follows.
// Measuring it in the following word's font makes the gap before a run of
// inline code more than twice as wide as a normal space, because Courier's
// space is 600/1000 em against Helvetica's 278 -- visible as a stutter in
// "injected by  dev and never".
func gapWidth(prev, next pdfWord) float64 {
	w := prev.font.width(' ', prev.size)
	if next.font == prev.font {
		return w
	}
	// Between two different fonts, the narrower space reads better than either
	// font's own.
	if other := next.font.width(' ', next.size); other < w {
		return other
	}
	return w
}

// inlineWords is the entry point used by the block renderer.
func inlineWords(list []Inline, base pdfFont, size float64, link string) []pdfWord {
	wb := &wordBuilder{size: size}
	wb.inlines(list, base, link)
	return wb.words
}

// Type sizes and leading, in points.
const (
	bodySize    = 10.5
	bodyLeading = 14
	codeSize    = 9
	codeLeading = 11.5
)

func headingSize(level int) float64 {
	switch level {
	case 1:
		return 19
	case 2:
		return 14.5
	case 3:
		return 12.5
	}
	return 11
}

// blocks renders a sequence of blocks at the given indent.
func (d *pdfDoc) blocks(blocks []*Block, indent float64) {
	for _, b := range blocks {
		d.block(b, indent)
	}
}

func (d *pdfDoc) block(b *Block, indent float64) {
	switch b.Kind {
	case KindHeading:
		size := headingSize(b.Level)
		// A heading at the bottom of a page is worse than a short page: keep it
		// with at least two lines of what follows.
		d.ensure(size*1.6 + bodyLeading*2)
		d.y -= size * 0.7
		d.paragraph(inlineWords(b.Inlines, fontBold, size, ""), indent, size*1.35)
		d.y -= size * 0.25

	case KindParagraph:
		d.paragraph(inlineWords(b.Inlines, fontRegular, bodySize, ""), indent, bodyLeading)
		d.y -= bodyLeading * 0.45

	case KindCodeFenced, KindCodeIndented:
		// A diagram has no place on paper as source. It becomes its own
		// accessible description, which is the same text a screen reader is
		// given for the SVG on the web.
		if isDiagramLanguage(firstWord(b.Info)) {
			if dia := parseDiagram(b.Text()); dia != nil {
				d.paragraph(oneWord(toASCII(dia.description()), fontItalic, bodySize-0.5, 0.4),
					indent+8, bodyLeading)
				d.y -= bodyLeading * 0.4
				return
			}
		}
		d.codeBlock(b, indent)

	case KindQuote:
		// A rule down the left margin, drawn after the content so that its
		// height is known.
		top := d.y
		d.blocks(b.Children, indent+16)
		d.rect(pdfMargin+indent+4, d.y+bodyLeading*0.4, 1.6, top-d.y-bodyLeading*0.4, 0.75)

	case KindList:
		d.list(b, indent)

	case KindThematicBreak:
		d.ensure(bodyLeading)
		d.y -= bodyLeading * 0.6
		d.rect(pdfMargin+indent, d.y, pdfTextWidth-indent, 0.6, 0.8)
		d.y -= bodyLeading * 0.6

	case KindHTMLBlock:
		// Raw HTML has no meaning on paper; skipping it is more honest than
		// printing angle brackets at the reader.
	}
}

// codeBlock renders literal text in the monospaced font on a tinted panel.
func (d *pdfDoc) codeBlock(b *Block, indent float64) {
	lines := strings.Split(strings.TrimRight(b.Text(), "\n"), "\n")

	// Long lines are hard-wrapped rather than clipped: a truncated command is
	// worse than an ugly one.
	var wrapped []string
	limit := pdfTextWidth - indent - 16
	for _, line := range lines {
		line = toASCII(strings.ReplaceAll(line, "\t", "    "))
		for fontMono.measure(line, codeSize) > limit && len(line) > 1 {
			cut := len(line)
			for cut > 1 && fontMono.measure(line[:cut], codeSize) > limit {
				cut--
			}
			wrapped = append(wrapped, line[:cut])
			line = line[cut:]
		}
		wrapped = append(wrapped, line)
	}

	d.y -= bodyLeading * 0.3
	for _, line := range wrapped {
		d.ensure(codeLeading + 6)
		// The panel is drawn per line so that a code block can cross a page
		// boundary without the background being left behind on the first page.
		d.rect(pdfMargin+indent, d.y-codeLeading+3, pdfTextWidth-indent, codeLeading, 0.955)
		d.showText(line, fontMono, codeSize, pdfMargin+indent+8, d.y-codeLeading+6.5, 0.1)
		d.y -= codeLeading
	}
	d.y -= bodyLeading * 0.5
}

// list renders a bullet or ordered list.
func (d *pdfDoc) list(b *Block, indent float64) {
	const markerWidth = 16

	for i, item := range b.Children {
		d.ensure(bodyLeading)
		markerY := d.y - bodyLeading*0.8

		if b.Ordered {
			label := strconv.Itoa(b.Start+i) + "."
			d.showText(label, fontRegular, bodySize, pdfMargin+indent, markerY, 0.35)
		} else {
			// A drawn dot rather than a bullet character: the base-14 fonts
			// have no bullet at an ASCII code point.
			d.rect(pdfMargin+indent+4, markerY+2.2, 2.6, 2.6, 0.35)
		}

		before := len(d.pages)
		d.blocks(item.Children, indent+markerWidth)

		// If the item began at the very bottom of a page, the marker was drawn
		// on the previous one. Nothing to be done about it after the fact, but
		// the tight spacing below keeps items from starting that low.
		_ = before

		if !b.Tight {
			d.y -= bodyLeading * 0.3
		}
	}
	d.y -= bodyLeading * 0.3
}

// isAbsoluteURL reports whether a link target has a scheme, which is the only
// kind of link that means anything once the document is a single file on disk.
func isAbsoluteURL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ':':
			return i > 0
		case isAlpha(c) || isDigit(c) || c == '+' || c == '-' || c == '.':
			continue
		default:
			return false
		}
	}
	return false
}

// RenderPDF renders a whole site as a single document.
func RenderPDF(s *Site, title string, cover bool) []byte {
	d := newPDFDoc()

	if cover {
		d.y = pdfPageHeight * 0.62
		d.paragraph(oneWord(toASCII(title), fontBold, 30), 0, 36)
		d.y -= 6
		d.paragraph(oneWord(strconv.Itoa(len(s.Pages))+" pages", fontRegular, 11, 0.45), 0, 16)
	}

	for i, page := range s.Pages {
		if cover || i > 0 {
			d.newPage()
		}
		d.blocks(page.Doc.Root.Children, 0)
	}

	return d.bytes()
}

// oneWord is a convenience for single-word lines such as the cover title.
func oneWord(text string, font pdfFont, size float64, grey ...float64) []pdfWord {
	g := 0.0
	if len(grey) > 0 {
		g = grey[0]
	}
	var words []pdfWord
	for i, field := range strings.Fields(text) {
		words = append(words, pdfWord{
			text: field, font: font, size: size, grey: g,
			glue: i == 0, width: font.measure(field, size),
		})
	}
	return words
}

// bytes serialises the document.
//
// Object numbering is fixed so that forward references can be written before
// the objects they point at: 1 is the catalogue, 2 the page tree, 3 to 6 the
// fonts, and pages occupy pairs from 7 onwards.
//
// There is deliberately no /Info dictionary. The obvious thing to put in one is
// a creation date, and a timestamp would make the output differ between runs --
// which would break the reproducibility this project claims elsewhere.
func (d *pdfDoc) bytes() []byte {
	const firstPageObj = 7
	var out bytes.Buffer
	offsets := make([]int, 1, 8+2*len(d.pages))

	obj := func(n int, body string) {
		for len(offsets) <= n {
			offsets = append(offsets, 0)
		}
		offsets[n] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	// The binary comment on line two tells tools this file is not plain text.
	out.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")

	var kids strings.Builder
	for i := range d.pages {
		if i > 0 {
			kids.WriteString(" ")
		}
		fmt.Fprintf(&kids, "%d 0 R", firstPageObj+i*2)
	}

	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(d.pages), kids.String()))
	for i, f := range []pdfFont{fontRegular, fontBold, fontItalic, fontMono} {
		obj(3+i, fmt.Sprintf(
			"<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>",
			f.baseFont()))
	}

	for i, page := range d.pages {
		pageObj := firstPageObj + i*2
		contentObj := pageObj + 1

		// A footer with the page number, added here because the total is only
		// known once every page exists.
		footer := fmt.Sprintf("%d", i+1)
		fmt.Fprintf(&page.content,
			"0.55 0.55 0.55 rg\nBT /F1 8.5 Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n",
			pdfPageWidth/2, pdfMargin*0.55, footer)

		annots := ""
		if len(page.annots) > 0 {
			annots = " /Annots [" + strings.Join(page.annots, " ") + "]"
		}
		obj(pageObj, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] "+
				"/Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R /F4 6 0 R >> >> "+
				"/Contents %d 0 R%s >>",
			pdfPageWidth, pdfPageHeight, contentObj, annots))

		stream := page.content.String()
		obj(contentObj, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}

	// The cross-reference table: one fixed-width entry per object, in order.
	// Every entry is exactly twenty bytes, which readers rely on.
	xrefOffset := out.Len()
	count := len(offsets)
	fmt.Fprintf(&out, "xref\n0 %d\n", count)
	out.WriteString("0000000000 65535 f \n")
	for n := 1; n < count; n++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		count, xrefOffset)

	return out.Bytes()
}
