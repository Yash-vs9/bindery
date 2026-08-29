package main

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Phase 2: inline structure.
//
// This pass turns the raw text of a leaf block into inline nodes. It is
// character-oriented, and it is where the specification stops being a
// description and becomes an algorithm you have to follow exactly.
//
// The hard part is emphasis. In "*foo **bar** baz*" the delimiters do not pair
// up by position, and whether a given "*" may open or close emphasis at all
// depends on the characters either side of it -- the flanking rules. No amount
// of matching or backtracking expresses that cleanly, so the specification
// describes a delimiter stack: scan left to right pushing every delimiter run,
// then walk the stack pairing closers with the nearest eligible opener. That is
// what processEmphasis does.
//
// Nodes are built as a doubly-linked list rather than a slice, because pairing
// emphasis means wrapping an arbitrary run of already-built nodes inside a new
// parent, and splicing a range out of a linked list is O(1) where re-slicing is
// not. The list is flattened into []Inline once, at the end.

// inode is a node in the working linked list.
type inode struct {
	kind        InlineKind
	text        string
	dest, title string
	first       *inode // first child, for emphasis and links
	prev, next  *inode
}

// delim is an entry on the delimiter stack. It points at the text node holding
// the delimiter run, so that consuming delimiters can shorten that text.
type delim struct {
	node       *inode
	char       byte
	count      int // delimiters still unconsumed
	origCount  int // run length as written, needed for the rule of three
	canOpen    bool
	canClose   bool
	prev, next *delim
}

// bracket is an entry on the bracket stack, used to pair "]" with "[".
type bracket struct {
	node     *inode // the text node holding "[" or "!["
	after    int    // offset in s just past the bracket
	image    bool
	active   bool // a link inside a link is not a link; this turns them off
	delimTop *delim
	prev     *bracket
}

type inlineParser struct {
	s    string
	pos  int
	refs map[string]linkRef

	head, tail *inode
	delims     *delim   // top of the delimiter stack
	brackets   *bracket // top of the bracket stack
}

// parseInlines converts the raw text of one leaf block into inline nodes.
func parseInlines(s string, refs map[string]linkRef) []Inline {
	p := &inlineParser{s: s, refs: refs}
	for p.pos < len(p.s) {
		p.step()
	}
	p.processEmphasis(nil)
	return flatten(p.head)
}

// step consumes one construct at the current position.
func (p *inlineParser) step() {
	switch c := p.s[p.pos]; c {
	case '\\':
		p.parseBackslash()
	case '`':
		p.parseCodeSpan()
	case '*', '_':
		p.parseDelimiterRun(c)
	case '[':
		p.pos++
		node := p.appendText("[")
		p.pushBracket(node, false)
	case '!':
		if p.pos+1 < len(p.s) && p.s[p.pos+1] == '[' {
			p.pos += 2
			node := p.appendText("![")
			p.pushBracket(node, true)
			return
		}
		p.pos++
		p.appendText("!")
	case ']':
		p.pos++
		p.parseCloseBracket()
	case '<':
		p.parseAngle()
	case '&':
		p.parseEntity()
	case '\n':
		p.parseLineBreak()
	default:
		p.parseText()
	}
}

// parseText consumes a run of ordinary characters. Stopping at every character
// that could begin a construct keeps the dispatch in step exhaustive.
func (p *inlineParser) parseText() {
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune("\\`*_[]!<&\n", rune(p.s[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		p.pos++ // a lone dispatch character that no rule claimed
	}
	p.appendText(p.s[start:p.pos])
}

// parseBackslash handles escapes. A backslash escapes ASCII punctuation and
// nothing else, so "\a" is two literal characters while "\*" is one. A
// backslash at end of line is a hard break.
func (p *inlineParser) parseBackslash() {
	p.pos++
	if p.pos >= len(p.s) {
		p.appendText("\\")
		return
	}
	c := p.s[p.pos]
	switch {
	case c == '\n':
		p.pos++
		p.append(&inode{kind: InlineHardBreak})
	case isASCIIPunct(c):
		p.pos++
		p.appendText(string(c))
	default:
		p.appendText("\\")
	}
}

// parseCodeSpan handles `code`. The opening and closing runs must be the same
// length, which is what lets "“ ` “" hold a backtick.
func (p *inlineParser) parseCodeSpan() {
	start := p.pos
	open := 0
	for p.pos < len(p.s) && p.s[p.pos] == '`' {
		open++
		p.pos++
	}
	contentStart := p.pos
	for p.pos < len(p.s) {
		if p.s[p.pos] != '`' {
			p.pos++
			continue
		}
		runStart := p.pos
		n := 0
		for p.pos < len(p.s) && p.s[p.pos] == '`' {
			n++
			p.pos++
		}
		if n == open {
			content := p.s[contentStart:runStart]
			// Line endings inside a code span become spaces.
			content = strings.ReplaceAll(content, "\n", " ")
			// One leading and one trailing space are stripped together, but only
			// if the content is not entirely spaces.
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' &&
				strings.Trim(content, " ") != "" {
				content = content[1 : len(content)-1]
			}
			p.append(&inode{kind: InlineCode, text: content})
			return
		}
	}
	// No closing run of the same length: the backticks are literal text.
	p.pos = contentStart
	p.appendText(p.s[start:contentStart])
}

// parseDelimiterRun pushes a run of * or _ onto the delimiter stack.
func (p *inlineParser) parseDelimiterRun(c byte) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] == c {
		p.pos++
	}
	run := p.pos - start
	before := prevRune(p.s, start)
	after := nextRune(p.s, p.pos)

	// Flanking rules. A run is left-flanking if it is not followed by
	// whitespace, and either not followed by punctuation or else preceded by
	// whitespace or punctuation. Right-flanking is the mirror image.
	afterWS := after == 0 || unicode.IsSpace(after)
	beforeWS := before == 0 || unicode.IsSpace(before)
	afterPunct := isPunct(after)
	beforePunct := isPunct(before)

	left := !afterWS && (!afterPunct || beforeWS || beforePunct)
	right := !beforeWS && (!beforePunct || afterWS || afterPunct)

	canOpen, canClose := left, right
	if c == '_' {
		// Underscore does not open or close inside a word, so that
		// snake_case_names survive.
		canOpen = left && (!right || beforePunct)
		canClose = right && (!left || afterPunct)
	}

	node := p.appendText(p.s[start:p.pos])
	p.pushDelim(&delim{
		node: node, char: c, count: run, origCount: run,
		canOpen: canOpen, canClose: canClose,
	})
}

// parseLineBreak distinguishes a hard break from a soft one. Two or more
// trailing spaces mean a hard break; anything else is a soft break, and the
// trailing whitespace is dropped either way.
func (p *inlineParser) parseLineBreak() {
	p.pos++
	hard := false
	if p.tail != nil && p.tail.kind == InlineText {
		trimmed := strings.TrimRight(p.tail.text, " ")
		if len(p.tail.text)-len(trimmed) >= 2 {
			hard = true
		}
		p.tail.text = trimmed
	}
	// Leading whitespace on the next line is not content.
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
	if hard {
		p.append(&inode{kind: InlineHardBreak})
	} else {
		p.append(&inode{kind: InlineSoftBreak})
	}
}

// parseEntity resolves a character reference.
func (p *inlineParser) parseEntity() {
	if decoded, next, ok := decodeEntity(p.s, p.pos); ok {
		p.pos = next
		p.appendText(decoded)
		return
	}
	p.pos++
	p.appendText("&")
}

// decodeEntity reads a character reference at s[i:], returning the decoded text
// and the offset just past it.
//
// The standard library does the hard part: html.UnescapeString carries the
// whole HTML5 named-entity table, around 2,100 entries, which CommonMark
// requires and which would otherwise have to be embedded by hand. What it will
// not do is enforce the specification's shape rules, and it is more permissive
// than they are -- it happily decodes "&#87654321;", which CommonMark leaves
// literal because a decimal reference may carry at most seven digits. So the
// form is validated here and the table lookup is delegated.
func decodeEntity(s string, i int) (string, int, bool) {
	if i >= len(s) || s[i] != '&' {
		return "", i, false
	}
	j := i + 1

	// Numeric: &#nnn; with at most seven digits, or &#xhhh; with at most six.
	if j < len(s) && s[j] == '#' {
		j++
		hex := j < len(s) && (s[j] == 'x' || s[j] == 'X')
		if hex {
			j++
		}
		start := j
		for j < len(s) && isEntityDigit(s[j], hex) {
			j++
		}
		switch n := j - start; {
		case n == 0, hex && n > 6, !hex && n > 7:
			return "", i, false
		}
		if j >= len(s) || s[j] != ';' {
			return "", i, false
		}
		j++
		return html.UnescapeString(s[i:j]), j, true
	}

	// Named: the table decides, and an unknown name stays literal.
	start := j
	for j < len(s) && (isAlpha(s[j]) || isDigit(s[j])) {
		j++
	}
	if j == start || j >= len(s) || s[j] != ';' {
		return "", i, false
	}
	j++
	candidate := s[i:j]
	if decoded := html.UnescapeString(candidate); decoded != candidate {
		return decoded, j, true
	}
	return "", i, false
}

// resolveEntities decodes every character reference in s. It is used for link
// destinations, titles and code-fence info strings, which the inline parser
// never walks character by character.
func resolveEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); {
		if decoded, next, ok := decodeEntity(s, i); ok {
			sb.WriteString(decoded)
			i = next
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func isEntityDigit(c byte, hex bool) bool {
	if isDigit(c) {
		return true
	}
	return hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
}

func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

func isPunct(r rune) bool {
	if r == 0 {
		return false
	}
	if r < utf8.RuneSelf && isASCIIPunct(byte(r)) {
		return true
	}
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func prevRune(s string, i int) rune {
	if i <= 0 {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return r
}

func nextRune(s string, i int) rune {
	if i >= len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}
