package main

import (
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Terminal rendering.
//
// This is what glow and mdcat are for: reading a Markdown file without opening
// a browser. It reuses the same document model as the HTML renderer, so there
// is one parser and three back ends rather than three implementations.
//
// Two standard-library gaps show up here.
//
// The first is terminal width. Go has no way to ask: the ioctl behind
// TIOCGWINSZ is not exposed, and golang.org/x/term is a dependency. So bindery
// reads the COLUMNS environment variable, which shells export, and falls back
// to eighty columns. That is wrong when a window is resized without the shell
// updating COLUMNS, and it is the honest limit of what the standard library
// can do.
//
// The second is display width. A rune is not a column: CJK ideographs and most
// emoji occupy two, and combining marks occupy none. utf8.RuneCountInString
// counts runes, so wrapping by it misaligns anything outside Latin text.
// displayWidth below approximates the East Asian Wide and Fullwidth ranges
// directly. It is an approximation, not the Unicode width tables, and it is
// documented as one.

// ANSI styling beyond the basic colours in term.go.
const (
	ansiItalic    = "\x1b[3m"
	ansiUnderline = "\x1b[4m"
	ansiBlue      = "\x1b[34m"
	ansiMagenta   = "\x1b[35m"
	ansiYellow    = "\x1b[33m"
)

// terminalWidth returns the number of columns to wrap at.
func terminalWidth() int {
	if columns := os.Getenv("COLUMNS"); columns != "" {
		if n, err := strconv.Atoi(columns); err == nil && n > 20 {
			return min(n, 100)
		}
	}
	return 80
}

// ansiRenderer holds the state of one terminal rendering.
type ansiRenderer struct {
	sb     strings.Builder
	width  int
	colour bool
}

// RenderANSI renders a document for a terminal. colour is false when the output
// is redirected, in which case the structure survives but the escapes do not.
func RenderANSI(d *Document, width int, colour bool) string {
	r := &ansiRenderer{width: width, colour: colour}
	r.blocks(d.Root.Children, "")
	return strings.TrimLeft(r.sb.String(), "\n")
}

func (r *ansiRenderer) style(s string, codes ...string) string {
	if !r.colour || s == "" {
		return s
	}
	return strings.Join(codes, "") + s + ansiReset
}

func (r *ansiRenderer) blocks(blocks []*Block, prefix string) {
	for i, b := range blocks {
		if i > 0 {
			r.sb.WriteString(prefix + "\n")
		}
		r.block(b, prefix)
	}
}

func (r *ansiRenderer) block(b *Block, prefix string) {
	switch b.Kind {
	case KindHeading:
		text := r.inlines(b.Inlines)
		switch b.Level {
		case 1:
			r.line(prefix, r.style(strings.ToUpper(text), ansiBold, ansiCyan))
			r.line(prefix, r.style(strings.Repeat("=", min(displayWidth(text), r.width-len(prefix))), ansiCyan))
		case 2:
			r.line(prefix, r.style(text, ansiBold, ansiCyan))
			r.line(prefix, r.style(strings.Repeat("-", min(displayWidth(text), r.width-len(prefix))), ansiDim))
		default:
			r.line(prefix, r.style(text, ansiBold))
		}

	case KindParagraph:
		r.wrap(prefix, r.inlines(b.Inlines))

	case KindThematicBreak:
		r.line(prefix, r.style(strings.Repeat("─", max(r.width-len(prefix), 1)), ansiDim))

	case KindCodeFenced, KindCodeIndented:
		// Code is indented rather than wrapped: breaking a line of code at a
		// word boundary makes it wrong, not merely ugly.
		for _, line := range strings.Split(strings.TrimRight(b.Text(), "\n"), "\n") {
			r.line(prefix, "  "+r.style(line, ansiGreen))
		}

	case KindQuote:
		r.blocks(b.Children, prefix+r.style("│ ", ansiDim))

	case KindList:
		for i, item := range b.Children {
			if i > 0 && !b.Tight {
				r.sb.WriteString(prefix + "\n")
			}
			marker := "• "
			if b.Ordered {
				marker = strconv.Itoa(b.Start+i) + ". "
			}
			r.item(item, prefix, r.style(marker, ansiYellow), strings.Repeat(" ", len(marker)))
		}

	case KindHTMLBlock:
		for _, line := range strings.Split(strings.TrimRight(b.Text(), "\n"), "\n") {
			r.line(prefix, r.style(line, ansiDim))
		}

	case KindTable:
		r.table(b, prefix)
	}
}

// table renders a GFM table as a box-drawing grid. Column widths are computed
// with displayWidth rather than byte or rune count, so CJK text and combining
// marks pad correctly.
func (r *ansiRenderer) table(b *Block, prefix string) {
	rows := make([][]string, len(b.Children))
	widths := make([]int, len(b.Align))

	for i, row := range b.Children {
		rows[i] = make([]string, len(widths))
		for col := range widths {
			text := ""
			if col < len(row.Children) {
				text = r.inlines(row.Children[col].Inlines)
			}
			rows[i][col] = text
			if w := displayWidth(text); w > widths[col] {
				widths[col] = w
			}
		}
	}
	for i, w := range widths {
		widths[i] = max(w, 3)
	}

	rule := func(left, mid, right string) {
		var sb strings.Builder
		sb.WriteString(left)
		for i, w := range widths {
			if i > 0 {
				sb.WriteString(mid)
			}
			sb.WriteString(strings.Repeat("─", w+2))
		}
		sb.WriteString(right)
		r.line(prefix, r.style(sb.String(), ansiDim))
	}

	renderRow := func(cells []string, bold bool) {
		var sb strings.Builder
		sb.WriteString(r.style("│", ansiDim))
		for col, w := range widths {
			text := cells[col]
			pad := max(w-displayWidth(text), 0)
			if bold {
				text = r.style(text, ansiBold)
			}
			sb.WriteString(" " + text + strings.Repeat(" ", pad) + " " + r.style("│", ansiDim))
		}
		r.line(prefix, sb.String())
	}

	rule("┌", "┬", "┐")
	for i, row := range rows {
		renderRow(row, i == 0)
		if i == 0 {
			rule("├", "┼", "┤")
		}
	}
	rule("└", "┴", "┘")
}

// item renders a list item, whose first line carries the marker and whose
// continuation lines are indented to match.
func (r *ansiRenderer) item(item *Block, prefix, marker, indent string) {
	var inner ansiRenderer
	inner.width, inner.colour = r.width-displayWidth(indent), r.colour
	inner.blocks(item.Children, "")

	for i, line := range strings.Split(strings.TrimRight(inner.sb.String(), "\n"), "\n") {
		if i == 0 {
			r.sb.WriteString(prefix + marker + line + "\n")
			continue
		}
		r.sb.WriteString(prefix + indent + line + "\n")
	}
}

func (r *ansiRenderer) line(prefix, text string) {
	r.sb.WriteString(prefix + text + "\n")
}

// wrap emits text wrapped to the available width, breaking between words.
func (r *ansiRenderer) wrap(prefix, text string) {
	limit := max(r.width-displayWidth(prefix), 20)
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case displayWidth(line)+1+displayWidth(word) <= limit:
				line += " " + word
			default:
				r.line(prefix, line)
				line = word
			}
		}
		if line != "" {
			r.line(prefix, line)
		}
	}
}

func (r *ansiRenderer) inlines(inlines []Inline) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch in.Kind {
		case InlineText:
			sb.WriteString(in.Text)
		case InlineCode:
			sb.WriteString(r.style(in.Text, ansiGreen))
		case InlineEmph:
			sb.WriteString(r.style(r.inlines(in.Children), ansiItalic))
		case InlineStrong:
			sb.WriteString(r.style(r.inlines(in.Children), ansiBold))
		case InlineLink:
			sb.WriteString(r.style(r.inlines(in.Children), ansiUnderline, ansiBlue))
			if in.Dest != "" {
				sb.WriteString(" " + r.style("("+in.Dest+")", ansiDim))
			}
		case InlineImage:
			sb.WriteString(r.style("[image: "+plainText(in.Children)+"]", ansiMagenta))
		case InlineRawHTML:
			sb.WriteString(r.style(in.Text, ansiDim))
		case InlineSoftBreak:
			sb.WriteString("\n")
		case InlineHardBreak:
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// displayWidth approximates how many terminal columns a string occupies.
//
// It counts ANSI escape sequences as zero, combining marks as zero, and the
// East Asian Wide and Fullwidth ranges as two. It is not the full Unicode width
// tables -- those live in golang.org/x/text -- and it will be wrong for some
// emoji sequences and rare scripts. For wrapping prose it is close enough, and
// being approximately right without a dependency beats being exactly right with
// one.
func displayWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			// Skip a CSI escape sequence: ESC [ ... final byte in @ to ~.
			j := i + 1
			if j < len(s) && s[j] == '[' {
				for j++; j < len(s) && (s[j] < '@' || s[j] > '~'); j++ {
				}
			}
			i = min(j+1, len(s))
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
			// combining marks occupy no column
		case isWideRune(r):
			width += 2
		default:
			width++
		}
	}
	return width
}

// isWideRune reports whether a rune occupies two terminal columns.
func isWideRune(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0xA4CF, // CJK radicals through Yi
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE6F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F, // emoji
		r >= 0x1F900 && r <= 0x1F9FF,
		r >= 0x20000 && r <= 0x3FFFD: // CJK extensions
		return true
	}
	return false
}
