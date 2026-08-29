package main

import "strings"

// Link reference definitions.
//
//	[label]: /destination "optional title"
//
// These are the reason the parser has two passes. A definition may appear
// anywhere in the document, including after the paragraph that refers to it, so
// inline resolution cannot begin until every line has been read.
//
// Structurally they are not blocks. They can only appear at the start of what
// would otherwise be a paragraph, so they are harvested when a paragraph is
// finalised: leading definitions are consumed, and whatever text remains stays
// a paragraph. A paragraph that was nothing but definitions disappears.

// parseRefDefs consumes leading link reference definitions from text, adding
// them to refs, and returns the text that remains.
func parseRefDefs(text string, refs map[string]linkRef) string {
	for {
		rest, ok := parseOneRefDef(text, refs)
		if !ok {
			return text
		}
		text = rest
		if strings.TrimSpace(text) == "" {
			return ""
		}
	}
}

// parseOneRefDef consumes a single definition from the start of text.
func parseOneRefDef(text string, refs map[string]linkRef) (string, bool) {
	p := &inlineParser{s: text}

	// Up to three spaces of indentation; four would be code.
	for p.pos < 3 && p.pos < len(text) && text[p.pos] == ' ' {
		p.pos++
	}
	if p.pos >= len(text) || text[p.pos] != '[' {
		return "", false
	}

	label, ok := scanRefLabel(text, p.pos)
	if !ok {
		return "", false
	}
	p.pos += len(label) + 2 // the label plus its brackets

	if p.pos >= len(text) || text[p.pos] != ':' {
		return "", false
	}
	p.pos++

	if !skipSpaceWithAtMostOneNewline(p) {
		return "", false
	}
	// An empty destination is legal only when written as <>, so remember
	// whether the destination was bracketed before parsing it.
	bracketed := p.pos < len(text) && text[p.pos] == '<'
	dest, ok := p.parseDestination()
	if !ok || (dest == "" && !bracketed) {
		return "", false
	}

	// A title is optional and must be separated from the destination by
	// whitespace. If what follows a candidate title is not the end of the line,
	// the title was not a title -- the definition ends at the destination and
	// the rest belongs to the next construct.
	title := ""
	afterDest := p.pos
	if skipSpaceWithAtMostOneNewline(p) && p.pos > afterDest {
		if candidate, ok := p.parseTitle(); ok && restOfLineBlank(text, p.pos) {
			title = candidate
		} else {
			p.pos = afterDest
		}
	} else {
		p.pos = afterDest
	}

	if !restOfLineBlank(text, p.pos) {
		return "", false
	}

	// The first definition of a label wins; later ones are ignored.
	if key := normalizeLabel(label); key != "" {
		if _, exists := refs[key]; !exists {
			refs[key] = linkRef{dest: dest, title: title}
		}
	}

	end := lineEnd(text, p.pos)
	if end < len(text) {
		end++ // consume the newline
	}
	return text[end:], true
}

// scanRefLabel reads the bracketed label beginning at open, returning its
// contents. A label may span lines but may not contain an unescaped bracket,
// and the specification caps it at 999 characters.
func scanRefLabel(text string, open int) (string, bool) {
	for i := open + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '[':
			return "", false
		case ']':
			label := text[open+1 : i]
			if len(label) > 999 || strings.TrimSpace(label) == "" {
				return "", false
			}
			return label, true
		}
	}
	return "", false
}

// skipSpaceWithAtMostOneNewline consumes whitespace, allowing the definition to
// wrap once. Two newlines would mean a blank line, which ends the paragraph and
// therefore the definition.
func skipSpaceWithAtMostOneNewline(p *inlineParser) bool {
	newlines := 0
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t':
			p.pos++
		case '\n':
			newlines++
			if newlines > 1 {
				return false
			}
			p.pos++
		default:
			return true
		}
	}
	return true
}

// restOfLineBlank reports whether only whitespace remains before the next
// newline.
func restOfLineBlank(text string, from int) bool {
	for i := from; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t':
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

// lineEnd returns the offset of the newline at or after from, or len(text).
func lineEnd(text string, from int) int {
	if i := strings.IndexByte(text[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(text)
}
