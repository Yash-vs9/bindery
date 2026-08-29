package main

import "strings"

// Line scanners used by phase 1.
//
// Each scanner is a predicate with a side effect: it inspects the line at the
// cursor and, if the line is what it is looking for, consumes it and reports
// what it found. Scanners never build blocks -- tryStart does that -- so they
// are safe to call speculatively from startsNewBlock, which needs to know
// whether a line would start a block without actually starting one.

// scanATXHeading matches "### Heading ###" and consumes it.
func (p *blockParser) scanATXHeading(c *cursor) (level int, content string, ok bool) {
	save := c.save()
	for c.peek() == '#' && level < 7 {
		level++
		c.pos++
		c.col++
	}
	// One to six hashes, and the run must be followed by whitespace or end of
	// line. "#hashtag" is a paragraph, not a heading.
	if level < 1 || level > 6 || (!c.atEOL() && c.peek() != ' ' && c.peek() != '\t') {
		c.restore(save)
		return 0, "", false
	}
	text := strings.Trim(c.rest(), " \t")

	// A closing sequence of hashes is stripped, but only when it is preceded by
	// a space or forms the whole content: "# foo#" keeps its hash.
	if trimmed := strings.TrimRight(text, "#"); trimmed != text {
		if trimmed == "" {
			text = ""
		} else if last := trimmed[len(trimmed)-1]; last == ' ' || last == '\t' {
			text = strings.Trim(trimmed, " \t")
		}
	}
	c.pos = len(c.s)
	return level, text, true
}

// fence describes an opening code fence.
type fence struct {
	char   byte // '`' or '~'
	length int  // how many fence characters, at least three
	info   string
}

// scanFence matches an opening code fence and consumes the line.
func (p *blockParser) scanFence(c *cursor) *fence {
	ch := c.peek()
	if ch != '`' && ch != '~' {
		return nil
	}
	save := c.save()
	n := 0
	for c.peek() == ch {
		n++
		c.pos++
		c.col++
	}
	if n < 3 {
		c.restore(save)
		return nil
	}
	info := strings.Trim(c.rest(), " \t")

	// A backtick fence's info string may not contain a backtick, otherwise
	// "``` `` ```" would be read as a fence rather than as a code span.
	if ch == '`' && strings.ContainsRune(info, '`') {
		c.restore(save)
		return nil
	}
	c.pos = len(c.s)
	return &fence{char: ch, length: n, info: info}
}

// scanSetextUnderline matches a run of = or - under a paragraph and consumes it.
func (p *blockParser) scanSetextUnderline(c *cursor) (level int, ok bool) {
	ch := c.peek()
	if ch != '=' && ch != '-' {
		return 0, false
	}
	save := c.save()
	for c.peek() == ch {
		c.pos++
		c.col++
	}
	if !c.isBlank() {
		c.restore(save)
		return 0, false
	}
	c.pos = len(c.s)
	if ch == '=' {
		return 1, true
	}
	return 2, true
}

// scanThematicBreak matches three or more of - _ or *, spaces permitted between
// them, and consumes the line.
func (p *blockParser) scanThematicBreak(c *cursor) bool {
	ch := c.peek()
	if ch != '-' && ch != '_' && ch != '*' {
		return false
	}
	save := c.save()
	n := 0
	for !c.atEOL() {
		switch b := c.peek(); {
		case b == ch:
			n++
		case b == ' ' || b == '\t':
		default:
			c.restore(save)
			return false
		}
		c.pos++
		c.col++
	}
	if n < 3 {
		c.restore(save)
		return false
	}
	return true
}

// listMarker describes the bullet or number that opens a list item.
type listMarker struct {
	ordered bool
	char    byte // '-', '+', '*', or the delimiter '.' or ')'
	start   int  // the number an ordered marker carries
	width   int  // columns the marker itself occupies
}

// scanListMarker matches a bullet or ordered-list marker and consumes it.
//
// The marker is recognised at M1 because startsNewBlock needs it: a list marker
// interrupts a paragraph, which affects lazy continuation even before list
// blocks themselves are built. Building them is M3.
func (p *blockParser) scanListMarker(c *cursor) *listMarker {
	save := c.save()

	switch b := c.peek(); b {
	case '-', '+', '*':
		c.pos++
		c.col++
		if !c.atEOL() && c.peek() != ' ' && c.peek() != '\t' {
			c.restore(save)
			return nil
		}
		return &listMarker{char: b, width: 1}
	}

	// An ordered marker is at most nine digits followed by "." or ")".
	digits := 0
	n := 0
	for d := c.peek(); d >= '0' && d <= '9'; d = c.peek() {
		n = n*10 + int(d-'0')
		digits++
		c.pos++
		c.col++
		if digits > 9 {
			c.restore(save)
			return nil
		}
	}
	if digits == 0 {
		c.restore(save)
		return nil
	}
	delim := c.peek()
	if delim != '.' && delim != ')' {
		c.restore(save)
		return nil
	}
	c.pos++
	c.col++
	if !c.atEOL() && c.peek() != ' ' && c.peek() != '\t' {
		c.restore(save)
		return nil
	}
	return &listMarker{ordered: true, char: delim, start: n, width: digits + 1}
}
