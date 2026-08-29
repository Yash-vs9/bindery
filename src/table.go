package main

import "strings"

// GFM tables.
//
// CommonMark itself has no tables at all -- this is a GitHub Flavored Markdown
// extension, not part of the core specification, and it is treated that way
// throughout: table recognition is a parser option, off by default, so that
// "bindery spec" continues to measure the parser against unmodified CommonMark
// and the 652/652 conformance score never depends on a GFM extension being
// enabled or not. Only the site build and "bindery render" turn it on.
//
// Recognition mirrors how a setext heading is recognised: a table is only
// visible in retrospect, once the line *after* a paragraph's last line turns
// out to be a delimiter row. The paragraph's final line becomes the header;
// any earlier lines remain an ordinary paragraph before it. From there the
// table is a leaf, like a fenced code block -- it absorbs one row per
// following non-blank line until a blank line closes it.

// CellAlign is the alignment a delimiter-row column specifies.
type CellAlign byte

const (
	AlignNone CellAlign = iota
	AlignLeft
	AlignRight
	AlignCenter
)

var cellAlignNames = [...]string{"none", "left", "right", "center"}

func (a CellAlign) String() string {
	if int(a) < len(cellAlignNames) {
		return cellAlignNames[a]
	}
	return "none"
}

// MarshalText makes alignment readable in "bindery render --format=json"
// output, the same way BlockKind and InlineKind already do.
func (a CellAlign) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

// scanTableDelimiterRow reports whether the cursor sits on a valid GFM table
// delimiter row -- cells of only hyphens, optionally flanked by a colon -- and
// consumes the line if so.
//
// A delimiter row is REQUIRED to contain at least one literal pipe. Without
// that rule, a bare "---" under a one-line paragraph would parse as a valid
// single-column delimiter row and silently steal every setext heading in the
// document. GFM's own reference implementation enforces exactly this, which is
// how a one-column table must be written "|---|" rather than bare "---".
func scanTableDelimiterRow(c *cursor) ([]CellAlign, bool) {
	line := strings.TrimSpace(c.rest())
	if line == "" || !strings.ContainsRune(line, '|') {
		return nil, false
	}

	body := strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	parts := strings.Split(body, "|")
	if len(parts) == 0 {
		return nil, false
	}

	aligns := make([]CellAlign, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		left := strings.HasPrefix(part, ":")
		right := strings.HasSuffix(part, ":")
		dashes := strings.TrimSuffix(strings.TrimPrefix(part, ":"), ":")
		if dashes == "" || strings.Trim(dashes, "-") != "" {
			return nil, false // must be one or more hyphens and nothing else
		}
		switch {
		case left && right:
			aligns = append(aligns, AlignCenter)
		case right:
			aligns = append(aligns, AlignRight)
		case left:
			aligns = append(aligns, AlignLeft)
		default:
			aligns = append(aligns, AlignNone)
		}
	}

	c.pos = len(c.s)
	return aligns, true
}

// splitTableRow splits one row of raw text into cell contents.
//
// A pipe is a separator unless it is escaped with a backslash or sits inside a
// run of backticks. The backslash is left in place rather than resolved here:
// phase 2's inline parser already resolves "\|" to a literal pipe as part of
// its ordinary backslash-escape handling, so splitting is the only job this
// function has.
//
// The backtick handling is a single-toggle simplification, not full
// CommonMark code-span matching (which requires the closing run to be the same
// length as the opening one). A cell like “ `a|b` “ splits correctly; the
// rarer “ “ a|b ` “ “ -- an opening run of two backticks meant to hold a
// literal single backtick -- would not. README.md and STDLIB.md say so.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")

	var cells []string
	var cur strings.Builder
	inCode := false

	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && i+1 < len(line) && (line[i+1] == '|' || line[i+1] == '\\'):
			cur.WriteByte(c)
			cur.WriteByte(line[i+1])
			i++
		case c == '`':
			inCode = !inCode
			cur.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	return cells
}

// isTableRowLine reports whether a non-blank line should be absorbed as
// another row of an already-open table. GFM's own rule is permissive: any
// non-blank line continues a table, even one without a pipe at all, which then
// becomes a single-cell row. That permissiveness is preserved here -- the
// blank-line check that gates this call is what actually closes a table.
func isTableRowLine(c *cursor) bool { return !c.isBlank() }
