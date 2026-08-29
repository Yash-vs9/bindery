package main

import "strings"

// Phase 1: block structure.
//
// The algorithm follows the one the CommonMark specification describes, because
// the specification's own description is operational and deviating from it
// costs conformance. For each line:
//
//  1. Walk down the chain of currently-open container blocks, asking each
//     whether this line continues it. Stop at the first that says no.
//  2. If a container did not match, a paragraph directly inside the deepest
//     matched container may still swallow the line -- this is lazy
//     continuation, and it is why step 2 comes before closing anything.
//  3. Close whatever remains unmatched, then repeatedly try to start new
//     blocks at the current position.
//  4. Give what is left of the line to the deepest open leaf, creating a
//     paragraph if there is none.
//
// Leaf content is accumulated as raw lines here and left uninterpreted. Inline
// parsing happens later, in inline.go, once every line has been seen.

// normalize prepares source text for parsing. The specification requires line
// endings to be treated uniformly and forbids NUL, which must be replaced with
// U+FFFD rather than dropped, so that byte offsets in error messages stay
// meaningful.
func normalize(src string) string {
	if strings.ContainsAny(src, "\r\x00") {
		src = strings.ReplaceAll(src, "\r\n", "\n")
		src = strings.ReplaceAll(src, "\r", "\n")
		src = strings.ReplaceAll(src, "\x00", "�")
	}
	return src
}

// cursor is a position within one line that understands tab stops.
//
// Tabs are the classic trap here. The specification does not expand tabs to
// spaces; it says that where whitespace defines block structure, a tab behaves
// as though it advanced to the next multiple-of-four column. So the cursor
// tracks a display column alongside the byte offset, and indentation is
// measured in columns while content is sliced by bytes.
type cursor struct {
	s   string
	pos int // byte offset
	col int // display column, tab stops every 4

	// pending is the number of columns still owed by a tab at s[pos] that was
	// only partly consumed. A tab advances to the next multiple-of-four column,
	// and a container prefix can end inside one: in ">\t\tfoo" the quote marker
	// takes a single column of the first tab and the remaining three become
	// content. While pending is non-zero the cursor behaves as though those
	// spaces were really in the input.
	pending int
}

func newCursor(s string) cursor { return cursor{s: s} }

func (c *cursor) atEOL() bool { return c.pos >= len(c.s) && c.pending == 0 }

// rest returns the remainder of the line, with any partly consumed tab expanded
// to the spaces it still owes.
func (c *cursor) rest() string {
	if c.pending > 0 {
		return strings.Repeat(" ", c.pending) + c.s[c.pos+1:]
	}
	return c.s[c.pos:]
}

func (c *cursor) peek() byte {
	if c.pending > 0 {
		return ' '
	}
	if c.pos >= len(c.s) {
		return 0
	}
	return c.s[c.pos]
}

func (c *cursor) save() cursor     { return *c }
func (c *cursor) restore(s cursor) { *c = s }

// isBlank reports whether only whitespace remains on the line.
func (c *cursor) isBlank() bool {
	for i := c.pos; i < len(c.s); i++ {
		if c.s[i] != ' ' && c.s[i] != '\t' {
			return false
		}
	}
	return true
}

// tabStop returns the column a tab at column col advances to.
func tabStop(col int) int { return col + 4 - col%4 }

// indent returns the width in columns of the whitespace ahead of the cursor.
func (c *cursor) indent() int {
	width := c.pending
	col := c.col + c.pending
	i := c.pos
	if c.pending > 0 {
		i++ // the partly consumed tab is already accounted for
	}
	for ; i < len(c.s); i++ {
		switch c.s[i] {
		case ' ':
			col++
			width++
		case '\t':
			next := tabStop(col)
			width += next - col
			col = next
		default:
			return width
		}
	}
	return width
}

// skipIndent consumes up to max columns of whitespace and returns how many
// columns it consumed, splitting a tab when the limit falls inside one.
func (c *cursor) skipIndent(max int) int {
	consumed := 0
	for consumed < max {
		if c.pending > 0 {
			take := min(c.pending, max-consumed)
			c.pending -= take
			c.col += take
			consumed += take
			if c.pending == 0 {
				c.pos++ // the tab is now fully accounted for
			}
			continue
		}
		if c.pos >= len(c.s) {
			break
		}
		switch c.s[c.pos] {
		case ' ':
			c.pos++
			c.col++
			consumed++
		case '\t':
			width := tabStop(c.col) - c.col
			if width <= max-consumed {
				c.pos++
				c.col += width
				consumed += width
				continue
			}
			take := max - consumed
			c.pending = width - take
			c.col += take
			consumed += take
		default:
			return consumed
		}
	}
	return consumed
}

// skipSpaces consumes all remaining whitespace.
func (c *cursor) skipSpaces() { c.skipIndent(1 << 30) }

// blockParser holds the state of one pass over one document.
type blockParser struct {
	doc     *Block
	refs    map[string]linkRef
	lineNum int

	// unmatched is the deepest container this line still matched, when some
	// container below it did not. Everything under it must be closed before any
	// new block is added -- but not sooner, because lazy continuation needs the
	// paragraph underneath to still be open. So the close is deferred until
	// something actually creates a block.
	unmatched *Block
}

// parseBlocks runs phase 1 over src and returns the block tree together with
// the link reference definitions it collected.
func parseBlocks(src string) *Document {
	p := &blockParser{
		doc:  &Block{Kind: KindDocument, Line: 1, open: true},
		refs: map[string]linkRef{},
	}
	src = normalize(src)
	src = strings.TrimSuffix(src, "\n")
	for _, text := range strings.Split(src, "\n") {
		p.lineNum++
		p.incorporateLine(text)
	}
	p.closeOpen(p.doc)
	sweepDropped(p.doc)
	return &Document{Root: p.doc, Refs: p.refs}
}

// incorporateLine processes one line and records whether it left the deepest
// open block ending on a blank line, which is what tight-versus-loose list
// rendering is decided from.
func (p *blockParser) incorporateLine(text string) {
	tip, blank := p.incorporate(text)
	if tip != nil {
		p.markBlank(tip, blank)
	}
}

// markBlank records a trailing blank line on a block and its ancestors.
//
// Three kinds of block are excused. A blank line inside a block quote or a code
// fence is content, not a separator. And a list item whose first line is blank
// has not "ended with a blank line" -- it has not started yet.
func (p *blockParser) markBlank(b *Block, blank bool) {
	if b.Kind == KindQuote || b.Kind == KindCodeFenced ||
		(b.Kind == KindListItem && len(b.Children) == 0 && b.Line == p.lineNum) {
		blank = false
	}
	for cur := b; cur != nil; cur = cur.parent {
		cur.lastBlank = blank
	}
}

func (p *blockParser) incorporate(text string) (tip *Block, blank bool) {
	c := newCursor(text)

	// Step 1: follow the open containers as far as this line allows.
	container := p.doc
	allMatched := true
	for {
		next := openContainerChild(container)
		if next == nil {
			break
		}
		if !p.continueContainer(next, &c) {
			allMatched = false
			break
		}
		container = next
	}

	leaf := openLeafChild(container)

	// A leaf that consumes lines verbatim -- a code fence -- takes the line
	// before any new block start is considered, so that "# not a heading"
	// inside a fence stays literal.
	if allMatched && leaf != nil && p.continueVerbatimLeaf(leaf, &c) {
		return leaf, c.isBlank()
	}

	p.unmatched = nil
	if !allMatched {
		p.unmatched = container
	}

	// Step 2: start whatever this line starts. appendChild closes the unmatched
	// containers on the way.
	started := false
	for container.isContainer() {
		block, ok := p.tryStart(container, &c)
		if !ok {
			break
		}
		started = true
		if !block.isContainer() {
			return block, false
		}
		container = block
	}

	// Step 3: lazy continuation. A paragraph swallows an unprefixed line even
	// though its enclosing container did not match, which is what makes
	//
	//	> one
	//	two
	//
	// a single two-line paragraph inside the quote. It applies only when no new
	// block started, and the paragraph in question is the deepest open leaf in
	// the tree rather than a child of the container that matched -- here the
	// quote failed to match, so the deepest matched container is the document
	// while the paragraph is one level down.
	if !started && p.unmatched != nil && !c.isBlank() {
		if para := deepestOpenLeaf(p.doc); para != nil && para.Kind == KindParagraph {
			p.unmatched = nil // the containers stay open; the paragraph continues
			c.skipSpaces()
			para.lines = append(para.lines, c.rest())
			return para, false
		}
	}

	// Step 4: hand the remainder to the deepest open leaf.
	p.flushUnmatched()
	return p.addText(container, &c), c.isBlank()
}

// flushUnmatched closes the containers this line left behind.
func (p *blockParser) flushUnmatched() {
	if p.unmatched != nil {
		p.closeOpen(p.unmatched)
		p.unmatched = nil
	}
}

// deepestOpenContainer descends to the innermost open container, which matters
// when one line opens several at once: "- > text" starts a list, an item and a
// quote before any text is placed.
func deepestOpenContainer(b *Block) *Block {
	for {
		next := openContainerChild(b)
		if next == nil {
			return b
		}
		b = next
	}
}

// openContainerChild returns b's last child if it is an open container.
func openContainerChild(b *Block) *Block {
	last := b.lastChild()
	if last != nil && last.open && last.isContainer() {
		return last
	}
	return nil
}

// deepestOpenLeaf follows the chain of last children to the deepest open leaf,
// which is the block a lazily-continued line belongs to.
func deepestOpenLeaf(b *Block) *Block {
	for {
		last := b.lastChild()
		if last == nil || !last.open {
			return nil
		}
		if !last.isContainer() {
			return last
		}
		b = last
	}
}

// openLeafChild returns b's last child if it is an open leaf.
func openLeafChild(b *Block) *Block {
	last := b.lastChild()
	if last != nil && last.open && !last.isContainer() {
		return last
	}
	return nil
}

// continueContainer reports whether the line at c continues the container b,
// consuming b's prefix if so.
func (p *blockParser) continueContainer(b *Block, c *cursor) bool {
	switch b.Kind {
	case KindDocument:
		return true

	case KindQuote:
		save := c.save()
		if c.skipIndent(3); c.peek() != '>' {
			c.restore(save)
			return false
		}
		c.pos++
		c.col++
		// One optional column after the marker belongs to the marker, not the
		// content: "> foo" has content "foo", not " foo". Going through
		// skipIndent means a tab here is split rather than swallowed whole.
		if ch := c.peek(); ch == ' ' || ch == '\t' {
			c.skipIndent(1)
		}
		return true

	case KindList:
		return true // a list continues as long as one of its items does

	case KindListItem:
		if c.isBlank() {
			// A blank line continues an item that has content, but an item
			// whose very first line is blank ends immediately -- otherwise
			// "-\n\n  text" would swallow the following paragraph.
			return len(b.Children) > 0
		}
		if c.indent() >= b.contIndl {
			c.skipIndent(b.contIndl)
			return true
		}
		return false
	}
	return false
}

// continueVerbatimLeaf handles leaves that take lines literally. It reports
// whether it consumed the line.
func (p *blockParser) continueVerbatimLeaf(b *Block, c *cursor) bool {
	switch b.Kind {
	case KindCodeFenced:
		if p.isClosingFence(b, c) {
			p.close(b)
			return true
		}
		// Strip up to as much leading whitespace as the opening fence had.
		c.skipIndent(b.fenceIndent)
		b.lines = append(b.lines, c.rest())
		return true

	case KindHTMLBlock:
		if b.htmlEnd == nil {
			// Types 6 and 7 end at a blank line, which is not part of the
			// block, so it is left for the ordinary path to handle.
			if c.isBlank() {
				p.close(b)
				return false
			}
			b.lines = append(b.lines, c.rest())
			return true
		}
		// Types 1 to 5 include the line that closes them.
		b.lines = append(b.lines, c.rest())
		if b.htmlEnd(c.rest()) {
			p.close(b)
		}
		return true

	case KindCodeIndented:
		if c.isBlank() {
			// Blank lines inside indented code keep whatever is past the four
			// columns of indentation, so "      " contributes two spaces.
			c.skipIndent(4)
			b.lines = append(b.lines, c.rest())
			return true
		}
		if c.indent() >= 4 {
			c.skipIndent(4)
			b.lines = append(b.lines, c.rest())
			return true
		}
		p.close(b)
		return false
	}
	return false
}

// isClosingFence reports whether the line at c closes the fence opened by b.
func (p *blockParser) isClosingFence(b *Block, c *cursor) bool {
	save := c.save()
	defer func() { c.restore(save) }()
	if c.skipIndent(3); c.peek() != b.fenceChar {
		return false
	}
	n := 0
	for c.peek() == b.fenceChar {
		n++
		c.pos++
		c.col++
	}
	if n < b.fenceLen {
		return false
	}
	return c.isBlank()
}

// tryStart attempts to begin one new block inside container, returning the
// block it created.
func (p *blockParser) tryStart(container *Block, c *cursor) (*Block, bool) {
	if c.isBlank() {
		return nil, false
	}

	// Indentation of four or more columns means indented code, unless a
	// paragraph is open, in which case the line is a continuation of it.
	if c.indent() >= 4 {
		// Indented code cannot interrupt a paragraph -- and the paragraph that
		// matters is the deepest open leaf in the tree, not a child of the
		// container that matched. In "> foo" followed by an indented line, the
		// quote fails to match, so the deepest matched container is the
		// document while the paragraph is inside the quote.
		if para := deepestOpenLeaf(p.doc); para != nil && para.Kind == KindParagraph {
			return nil, false
		}
		c.skipIndent(4)
		b := p.appendChild(container, KindCodeIndented)
		b.lines = append(b.lines, c.rest())
		return b, true
	}

	save := c.save()
	indent := c.indent()
	if indent > 3 {
		indent = 3
	}
	c.skipIndent(3)

	// Block quote.
	if c.peek() == '>' {
		c.pos++
		c.col++
		if ch := c.peek(); ch == ' ' || ch == '\t' {
			c.skipIndent(1)
		}
		return p.appendChild(container, KindQuote), true
	}

	// ATX heading.
	if lvl, content, ok := p.scanATXHeading(c); ok {
		b := p.appendChild(container, KindHeading)
		b.Level = lvl
		b.lines = append(b.lines, content)
		p.close(b)
		return b, true
	}

	// HTML block. The raw line is taken from before the indentation was
	// consumed, because an HTML block's content is literal.
	if c.peek() == '<' {
		para := openLeafChild(container)
		inParagraph := para != nil && para.Kind == KindParagraph
		if end, ok := htmlBlockStart(c.rest(), inParagraph); ok {
			raw := c.s[save.pos:]
			b := p.appendChild(container, KindHTMLBlock)
			b.htmlEnd = end
			b.lines = append(b.lines, raw)
			if end != nil && end(raw) {
				p.close(b)
			}
			c.pos = len(c.s)
			return b, true
		}
	}

	// Fenced code.
	if f := p.scanFence(c); f != nil {
		b := p.appendChild(container, KindCodeFenced)
		b.fenceChar, b.fenceLen, b.fenceIndent, b.Info = f.char, f.length, indent, f.info
		return b, true
	}

	// Setext heading: an underline turns the open paragraph into a heading.
	// This is tried before thematic breaks so that "---" under a paragraph is a
	// level-two heading rather than a rule. The open-paragraph precondition is
	// checked first, because the scanner consumes the line and a consumed line
	// is invisible to the thematic-break scanner below.
	if para := openLeafChild(container); para != nil && para.Kind == KindParagraph {
		before := c.save()
		if lvl, ok := p.scanSetextUnderline(c); ok {
			// Strip any leading link reference definitions before deciding.
			// "[foo]: /url" followed by "===" is a definition and then a
			// paragraph of equals signs, not a heading.
			remaining := parseRefDefs(strings.Join(para.lines, "\n"), p.refs)
			if strings.TrimSpace(remaining) == "" {
				para.lines = nil
				para.dropped = true
				para.open = false
				c.restore(before)
				return nil, false
			}
			para.lines = strings.Split(remaining, "\n")
			p.flushUnmatched()
			para.Kind = KindHeading
			para.Level = lvl
			p.close(para)
			return para, true
		}
	}

	// Thematic break. Checked before list items so that "- - -" is a rule
	// rather than a list containing a rule.
	if p.scanThematicBreak(c) {
		b := p.appendChild(container, KindThematicBreak)
		p.close(b)
		return b, true
	}

	// List item.
	if marker := p.scanListMarker(c); marker != nil && p.listMayStart(container, marker, c) {
		contentIndent := indent + marker.width + listPadding(c)

		// An item joins the list above it only if the marker type matches:
		// switching from "-" to "*", or from "." to ")", starts a new list.
		list := container
		if list.Kind != KindList {
			if last := openContainerChild(container); last != nil && last.Kind == KindList {
				list = last
			}
		}
		if list.Kind == KindList && !list.matchesMarker(marker) {
			p.close(list)
			list = list.parent
		}
		if list.Kind != KindList {
			if !list.canContain(KindList) {
				c.restore(save)
				return nil, false
			}
			list = p.appendChild(list, KindList)
			list.Ordered = marker.ordered
			list.Start = marker.start
			list.Marker = marker.char
		}

		item := p.appendChild(list, KindListItem)
		item.Marker = marker.char
		item.contIndl = contentIndent
		return item, true
	}

	c.restore(save)
	return nil, false
}

// listPadding returns the columns between a list marker and its content, and
// consumes them.
//
// One to four spaces after the marker are part of the marker's padding. Five or
// more mean the content is an indented code block that happens to start on the
// same line, so only one space belongs to the marker and the rest is content.
// An empty item is treated as having a single space of padding.
func listPadding(c *cursor) int {
	if c.isBlank() {
		return 1
	}
	spaces := c.indent()
	if spaces < 1 || spaces > 4 {
		// Five or more columns mean the content is an indented code block that
		// happens to begin on the marker's line: one column belongs to the
		// marker and must be consumed, the rest is content.
		c.skipIndent(1)
		return 1
	}
	c.skipIndent(spaces)
	return spaces
}

// listMayStart reports whether a list item may begin here.
//
// The restriction exists so that ordinary prose does not fragment: a line in the
// middle of a paragraph that happens to begin "2. " is part of the paragraph,
// not a new list. The specification allows a list to interrupt a paragraph only
// when the item has content and, if ordered, is numbered 1.
func (p *blockParser) listMayStart(container *Block, marker *listMarker, c *cursor) bool {
	para := openLeafChild(container)
	if para == nil || para.Kind != KindParagraph {
		return true
	}
	if c.isBlank() {
		return false
	}
	return !marker.ordered || marker.start == 1
}

// matchesMarker reports whether an item with this marker belongs to list b.
func (b *Block) matchesMarker(m *listMarker) bool {
	return b.Ordered == m.ordered && b.Marker == m.char
}

// addText gives the rest of the line to the deepest open leaf of container and
// returns the block the line ended up in.
func (p *blockParser) addText(container *Block, c *cursor) *Block {
	leaf := openLeafChild(container)

	if c.isBlank() {
		// Record the blank on the block it followed. A list is loose when an
		// item's children are separated by a blank line, so the separated child
		// is what must remember it -- marking only the container would lose the
		// distinction between "- foo\n\n  bar" and "- foo\n  bar".
		if last := container.lastChild(); last != nil {
			last.lastBlank = true
		}
		if leaf != nil && leaf.Kind == KindParagraph {
			p.close(leaf)
			return container
		}
		if leaf != nil {
			return leaf
		}
		return container
	}

	if leaf == nil {
		c.skipSpaces()
		leaf = p.appendChild(container, KindParagraph)
	} else {
		c.skipSpaces()
	}
	leaf.lines = append(leaf.lines, c.rest())
	return leaf
}

func (p *blockParser) appendChild(parent *Block, kind BlockKind) *Block {
	p.flushUnmatched()

	// Climb until a parent can hold this kind of block, closing what cannot.
	// A paragraph following a list is the case that matters: the list is the
	// deepest matched container but cannot contain a paragraph, so it is
	// finalised and the paragraph becomes a sibling of the list.
	for parent.parent != nil && !parent.canContain(kind) {
		p.close(parent)
		parent = parent.parent
	}

	// Only one leaf may be open at a time within a parent.
	if last := parent.lastChild(); last != nil && last.open && !last.isContainer() {
		p.close(last)
	}
	b := &Block{Kind: kind, Line: p.lineNum, open: true, parent: parent}
	parent.Children = append(parent.Children, b)
	return b
}

// close finalises one block.
func (p *blockParser) close(b *Block) {
	if !b.open {
		return
	}
	b.open = false

	// A paragraph may begin with link reference definitions. They are harvested
	// here, when the paragraph's text is complete, and what remains stays a
	// paragraph -- or the paragraph disappears if that was all it held.
	if b.Kind == KindParagraph {
		remaining := parseRefDefs(strings.Join(b.lines, "\n"), p.refs)
		if strings.TrimSpace(remaining) == "" {
			b.lines = nil
			b.dropped = true
		} else {
			b.lines = strings.Split(remaining, "\n")
		}
	}

	if b.Kind == KindList {
		b.Tight = isTightList(b)
	}
	if b.Kind == KindCodeIndented || b.Kind == KindHTMLBlock {
		// Trailing blank lines belong to the document, not to the block.
		for len(b.lines) > 0 && strings.TrimSpace(b.lines[len(b.lines)-1]) == "" {
			b.lines = b.lines[:len(b.lines)-1]
		}
	}
}

// closeOpen closes every open descendant of b, deepest first.
func (p *blockParser) closeOpen(b *Block) {
	for _, child := range b.Children {
		if child.open {
			p.closeOpen(child)
			p.close(child)
		}
	}
}

// isTightList decides how a list renders.
//
// A tight list puts item content directly inside <li>; a loose one wraps each
// item's content in <p>. The specification defines looseness structurally: a
// list is loose if any of its items are separated by a blank line, or if any
// item contains two block-level children separated by one. Trailing blank lines
// at the very end of the list do not count, which is why both checks skip the
// final position.
func isTightList(list *Block) bool {
	for i, item := range list.Children {
		last := i == len(list.Children)-1
		if endsWithBlankLine(item) && !last {
			return false
		}
		for j, child := range item.Children {
			if endsWithBlankLine(child) && !(last && j == len(item.Children)-1) {
				return false
			}
		}
	}
	return true
}

// endsWithBlankLine reports whether b, or the deepest block at its end, was left
// on a blank line.
func endsWithBlankLine(b *Block) bool {
	for cur := b; cur != nil; {
		if cur.lastBlank {
			return true
		}
		if cur.Kind != KindList && cur.Kind != KindListItem {
			return false
		}
		cur = cur.lastChild()
	}
	return false
}

// sweepDropped removes paragraphs that held nothing but link reference
// definitions.
//
// They are marked during finalisation rather than removed there, because
// finalisation walks the children of each block and mutating that slice while
// walking it is how nodes go missing. One pass afterwards is simpler to reason
// about than a careful in-place delete.
func sweepDropped(b *Block) {
	kept := b.Children[:0]
	for _, child := range b.Children {
		if child.dropped {
			continue
		}
		sweepDropped(child)
		kept = append(kept, child)
	}
	b.Children = kept
}
