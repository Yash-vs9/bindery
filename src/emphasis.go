package main

import "strings"

// The linked-list plumbing and the emphasis algorithm.

func (p *inlineParser) append(n *inode) {
	if p.tail == nil {
		p.head, p.tail = n, n
		return
	}
	n.prev = p.tail
	p.tail.next = n
	p.tail = n
}

// appendText adds a text node and returns it, so that callers who need to refer
// back to it -- delimiter runs and brackets do -- can hold the pointer.
func (p *inlineParser) appendText(s string) *inode {
	n := &inode{kind: InlineText, text: s}
	p.append(n)
	return n
}

func (p *inlineParser) removeNode(n *inode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		p.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		p.tail = n.prev
	}
	n.prev, n.next = nil, nil
}

func (p *inlineParser) pushDelim(d *delim) {
	d.prev = p.delims
	if p.delims != nil {
		p.delims.next = d
	}
	p.delims = d
}

func (p *inlineParser) removeDelim(d *delim) {
	if d.prev != nil {
		d.prev.next = d.next
	}
	if d.next != nil {
		d.next.prev = d.prev
	} else {
		p.delims = d.prev
	}
	d.prev, d.next = nil, nil
}

func (p *inlineParser) pushBracket(node *inode, image bool) {
	p.brackets = &bracket{
		node: node, after: p.pos, image: image,
		active: true, delimTop: p.delims, prev: p.brackets,
	}
}

func (p *inlineParser) popBracket() {
	if p.brackets != nil {
		p.brackets = p.brackets.prev
	}
}

// processEmphasis pairs closers with openers on the delimiter stack, from the
// entry just above bottom to the top. Passing a bottom limits the pass to the
// delimiters opened inside a link's text, which is why link parsing calls it
// with the stack top it captured when the bracket was pushed.
//
// This is the specification's algorithm, including its openersBottom
// bookkeeping: once a closer of a given character, run length modulo three and
// can-open status has failed to find an opener, no later closer with the same
// signature needs to look further back than that point. Without it, pathological
// input like a thousand asterisks is quadratic.
func (p *inlineParser) processEmphasis(bottom *delim) {
	var openersBottom [2][3][2]*delim
	for i := range openersBottom {
		for j := range openersBottom[i] {
			for k := range openersBottom[i][j] {
				openersBottom[i][j][k] = bottom
			}
		}
	}

	// Walk down to the first delimiter above bottom, then work upwards.
	closer := p.delims
	for closer != nil && closer.prev != bottom {
		closer = closer.prev
	}

	for closer != nil {
		if !closer.canClose {
			closer = closer.next
			continue
		}

		ci, cm, cc := charIndex(closer.char), closer.origCount%3, boolIndex(closer.canOpen)
		limit := openersBottom[ci][cm][cc]

		var opener *delim
		for d := closer.prev; d != nil && d != bottom && d != limit; d = d.prev {
			if d.char == closer.char && d.canOpen && canPair(d, closer) {
				opener = d
				break
			}
		}

		if opener == nil {
			// Record how far back this signature had to look, then move on. A
			// closer that cannot also open is spent and leaves the stack.
			openersBottom[ci][cm][cc] = closer.prev
			next := closer.next
			if !closer.canOpen {
				p.removeDelim(closer)
			}
			closer = next
			continue
		}

		// Two delimiters make strong emphasis, one makes regular emphasis.
		use := 1
		kind := InlineEmph
		if opener.count >= 2 && closer.count >= 2 {
			use = 2
			kind = InlineStrong
		}

		p.wrap(opener.node, closer.node, kind)

		// The delimiters closest to the content are the ones consumed: the tail
		// of the opening run and the head of the closing run.
		opener.node.text = opener.node.text[:len(opener.node.text)-use]
		closer.node.text = closer.node.text[use:]
		opener.count -= use
		closer.count -= use

		// Delimiters trapped between a matched pair can never match anything.
		for d := opener.next; d != nil && d != closer; {
			next := d.next
			p.removeDelim(d)
			d = next
		}

		if opener.count == 0 {
			p.removeNode(opener.node)
			p.removeDelim(opener)
		}
		if closer.count == 0 {
			next := closer.next
			p.removeNode(closer.node)
			p.removeDelim(closer)
			closer = next
		}
	}

	for p.delims != nil && p.delims != bottom {
		p.removeDelim(p.delims)
	}
}

// canPair applies the specification's rule of three: if either delimiter of a
// candidate pair can both open and close, the two run lengths may not sum to a
// multiple of three unless both are themselves multiples of three. It exists so
// that "*foo**bar*" resolves the way readers expect.
func canPair(opener, closer *delim) bool {
	if !closer.canOpen && !opener.canClose {
		return true
	}
	if closer.origCount%3 == 0 && opener.origCount%3 == 0 {
		return true
	}
	return (opener.origCount+closer.origCount)%3 != 0
}

// wrap moves every node strictly between from and to into a new node of the
// given kind, and splices that node into their place.
func (p *inlineParser) wrap(from, to *inode, kind InlineKind) {
	parent := &inode{kind: kind}
	var first, last *inode
	for n := from.next; n != nil && n != to; {
		next := n.next
		n.prev, n.next = nil, nil
		if first == nil {
			first, last = n, n
		} else {
			last.next = n
			n.prev = last
			last = n
		}
		n = next
	}
	parent.first = first
	parent.prev, parent.next = from, to
	from.next = parent
	to.prev = parent
}

func charIndex(c byte) int {
	if c == '_' {
		return 1
	}
	return 0
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

// flatten converts the working linked list into the exported slice form,
// merging adjacent text nodes and dropping the empty ones left behind by
// consumed delimiter runs.
func flatten(head *inode) []Inline {
	var out []Inline
	for n := head; n != nil; n = n.next {
		if n.kind == InlineText {
			if n.text == "" {
				continue
			}
			if len(out) > 0 && out[len(out)-1].Kind == InlineText {
				out[len(out)-1].Text += n.text
				continue
			}
		}
		out = append(out, Inline{
			Kind:     n.kind,
			Text:     n.text,
			Dest:     n.dest,
			Title:    n.title,
			Children: flatten(n.first),
		})
	}
	return out
}

// parseDocumentInlines runs phase 2 over every leaf block that holds prose.
// Code blocks are skipped: their content is literal by definition.
func parseDocumentInlines(d *Document) {
	var walk func(b *Block)
	walk = func(b *Block) {
		switch {
		case b.isContainer():
			for _, child := range b.Children {
				walk(child)
			}
		case b.Kind == KindCodeFenced || b.Kind == KindCodeIndented || b.Kind == KindHTMLBlock:
			// literal
		case b.Kind == KindThematicBreak:
			// no content
		default:
			b.Inlines = parseInlines(b.Text(), d.Refs)
		}
	}
	walk(d.Root)
}

// Parse runs both phases and returns a complete document.
func Parse(src string) *Document {
	doc := parseBlocks(src)
	parseDocumentInlines(doc)
	return doc
}

// normalizeLabel folds a link label for lookup: case-insensitive, with runs of
// whitespace collapsed to a single space.
// normalizeLabel folds a link label for lookup: full Unicode case folding
// (fullFold, in casefold.go) with runs of whitespace collapsed to one space.
//
// Simple strings.ToLower is not enough. CommonMark example 540 pairs "[SS]:" as
// a reference definition with "[ẞ]" as the use, and those only compare equal
// under full case folding -- ẞ (U+1E9E) folds to the two characters "ss", which
// strings.ToLower cannot produce because it maps one rune to one rune.
func normalizeLabel(s string) string {
	trimmed := strings.TrimSpace(s)
	var sb strings.Builder
	sb.Grow(len(trimmed))
	for _, r := range trimmed {
		sb.WriteString(fullFold(r))
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
