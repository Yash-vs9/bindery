package main

import "strings"

// HTML blocks.
//
// The specification defines seven kinds, distinguished by how they start and,
// more importantly, by how they end. Types 1 to 5 run to a specific closing
// string and include the line carrying it. Types 6 and 7 run to the next blank
// line, which is not part of the block. Type 7 alone cannot interrupt a
// paragraph, which is what stops an ordinary sentence containing a tag from
// swallowing the paragraph it sits in.
//
// A start returns the block's end condition as a function. A nil function means
// "ends at the next blank line", which keeps the two families distinguishable
// without a second field.

// htmlBlockTags is the set of element names that begin a type 6 HTML block.
// It is transcribed from the specification rather than inferred: HTML has no
// rule that generates this list, so it has to be written down.
var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "base": true,
	"basefont": true, "blockquote": true, "body": true, "caption": true,
	"center": true, "col": true, "colgroup": true, "dd": true, "details": true,
	"dialog": true, "dir": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "frame": true, "frameset": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true,
	"header": true, "hr": true, "html": true, "iframe": true, "legend": true,
	"li": true, "link": true, "main": true, "menu": true, "menuitem": true,
	"nav": true, "noframes": true, "ol": true, "optgroup": true,
	"option": true, "p": true, "param": true, "search": true, "section": true,
	"summary": true, "table": true, "tbody": true, "td": true, "tfoot": true,
	"th": true, "thead": true, "title": true, "tr": true, "track": true,
	"ul": true,
}

// htmlBlockStart classifies a line beginning with "<".
//
// It returns the end condition and whether a block starts at all. A nil end
// condition means the block ends at the next blank line. inParagraph suppresses
// type 7, which may not interrupt a paragraph.
func htmlBlockStart(line string, inParagraph bool) (func(string) bool, bool) {
	lower := strings.ToLower(line)

	// Type 1: raw-text elements, ending at their closing tag.
	for _, tag := range []string{"script", "pre", "style", "textarea"} {
		if strings.HasPrefix(lower, "<"+tag) {
			rest := lower[1+len(tag):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '>' {
				closing := "</" + tag + ">"
				return func(l string) bool {
					return strings.Contains(strings.ToLower(l), closing)
				}, true
			}
		}
	}

	// Types 2 to 5: comment, processing instruction, declaration, CDATA.
	for _, pair := range [][2]string{
		{"<!--", "-->"},
		{"<?", "?>"},
		{"<![CDATA[", "]]>"},
	} {
		if strings.HasPrefix(line, pair[0]) {
			closing := pair[1]
			return func(l string) bool { return strings.Contains(l, closing) }, true
		}
	}
	if len(line) > 2 && strings.HasPrefix(line, "<!") && isAlpha(line[2]) {
		return func(l string) bool { return strings.Contains(l, ">") }, true
	}

	// Type 6: a known block-level tag name, open or closing.
	name, after := htmlTagName(line)
	if name != "" && htmlBlockTags[strings.ToLower(name)] {
		switch {
		case after == "", strings.HasPrefix(after, " "), strings.HasPrefix(after, "\t"),
			strings.HasPrefix(after, ">"), strings.HasPrefix(after, "/>"):
			return nil, true
		}
	}

	// Type 7: any complete tag, alone on its line. It cannot interrupt a
	// paragraph, so that prose mentioning <span> stays one paragraph.
	if inParagraph {
		return nil, false
	}
	p := &inlineParser{s: line}
	if _, ok := p.parseRawHTML(); ok && strings.TrimSpace(line[p.pos:]) == "" {
		return nil, true
	}
	return nil, false
}

// htmlTagName returns the element name at the start of a tag and the text
// following it.
func htmlTagName(line string) (name, after string) {
	i := 1
	if i < len(line) && line[i] == '/' {
		i++
	}
	start := i
	for i < len(line) && (isAlpha(line[i]) || isDigit(line[i])) {
		i++
	}
	if i == start {
		return "", ""
	}
	return line[start:i], line[i:]
}
