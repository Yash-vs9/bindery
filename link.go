package main

import "strings"

// Links, images, autolinks and raw HTML.
//
// Links are the other construct that needs a stack. When "]" is reached the
// parser has to decide whether an earlier "[" opened a link, and the answer
// depends on what follows the bracket -- an inline destination, a reference
// label, or nothing at all. The bracket stack remembers each opening bracket
// along with the delimiter-stack top at the time, so that emphasis inside the
// link text can be resolved in isolation from emphasis outside it.

// parseCloseBracket handles "]", turning a bracket pair into a link or image if
// the syntax allows, and leaving literal text if it does not.
func (p *inlineParser) parseCloseBracket() {
	br := p.brackets
	if br == nil {
		p.appendText("]")
		return
	}

	// An inactive bracket is one that sits inside an enclosing link. Links do
	// not nest, so it can only be literal text.
	if !br.active {
		p.popBracket()
		p.appendText("]")
		return
	}

	labelEnd := p.pos - 1 // offset of the "]" itself
	save := p.pos

	dest, title, ok := p.parseInlineDestination()
	if !ok {
		dest, title, ok = p.parseReference(br, labelEnd)
	}
	if !ok {
		p.pos = save
		p.popBracket()
		p.appendText("]")
		return
	}

	kind := InlineLink
	if br.image {
		kind = InlineImage
	}

	// Resolve emphasis that opened inside the link text before capturing it, so
	// that unmatched delimiters there cannot pair with delimiters outside.
	p.processEmphasis(br.delimTop)

	link := &inode{kind: kind, dest: dest, title: title}
	var first, last *inode
	for n := br.node.next; n != nil; {
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
	link.first = first

	p.tail = br.node
	br.node.next = nil
	p.removeNode(br.node)
	p.append(link)

	// A link disables every enclosing bracket: "[a [b](c)](d)" has one link.
	// Images do not, because an image may sit inside a link.
	if !br.image {
		for b := br.prev; b != nil; b = b.prev {
			// Image brackets are left alone. "![foo [bar](/url)](/url2)" is an
			// image whose alt text contains a link, so the inner link must not
			// disable the enclosing "![".
			if !b.image {
				b.active = false
			}
		}
	}
	p.popBracket()
}

// parseInlineDestination parses "(dest)" or "(dest \"title\")" at the cursor.
func (p *inlineParser) parseInlineDestination() (dest, title string, ok bool) {
	if p.pos >= len(p.s) || p.s[p.pos] != '(' {
		return "", "", false
	}
	save := p.pos
	p.pos++
	p.skipInlineSpace()

	dest, ok = p.parseDestination()
	if !ok {
		p.pos = save
		return "", "", false
	}
	p.skipInlineSpace()

	if p.pos < len(p.s) && p.s[p.pos] != ')' {
		title, ok = p.parseTitle()
		if !ok {
			p.pos = save
			return "", "", false
		}
		p.skipInlineSpace()
	}

	if p.pos >= len(p.s) || p.s[p.pos] != ')' {
		p.pos = save
		return "", "", false
	}
	p.pos++
	return dest, title, true
}

// parseDestination reads a link destination, either wrapped in angle brackets
// or bare. A bare destination runs to whitespace, and its parentheses must
// balance so that "(a(b))" is one destination.
func (p *inlineParser) parseDestination() (string, bool) {
	if p.pos < len(p.s) && p.s[p.pos] == '<' {
		for i := p.pos + 1; i < len(p.s); i++ {
			switch p.s[i] {
			case '>':
				dest := p.s[p.pos+1 : i]
				p.pos = i + 1
				return unescapeBackslashes(dest), true
			case '<', '\n':
				return "", false
			case '\\':
				i++
			}
		}
		return "", false
	}

	start := p.pos
	depth := 0
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c == '\\' && p.pos+1 < len(p.s) && isASCIIPunct(p.s[p.pos+1]):
			p.pos++
		case c == '(':
			depth++
		case c == ')':
			if depth == 0 {
				return unescapeBackslashes(p.s[start:p.pos]), true
			}
			depth--
		case c == ' ' || c == '\t' || c == '\n':
			return unescapeBackslashes(p.s[start:p.pos]), depth == 0
		case c < 0x20:
			return "", false
		}
		p.pos++
	}
	return unescapeBackslashes(p.s[start:p.pos]), depth == 0
}

// parseTitle reads a link title delimited by ", ' or ().
func (p *inlineParser) parseTitle() (string, bool) {
	if p.pos >= len(p.s) {
		return "", false
	}
	open := p.s[p.pos]
	var close byte
	switch open {
	case '"':
		close = '"'
	case '\'':
		close = '\''
	case '(':
		close = ')'
	default:
		return "", false
	}
	for i := p.pos + 1; i < len(p.s); i++ {
		switch p.s[i] {
		case '\\':
			i++
		case close:
			title := p.s[p.pos+1 : i]
			p.pos = i + 1
			return unescapeBackslashes(title), true
		case open:
			if open == '(' {
				return "", false // nested unescaped parenthesis
			}
		}
	}
	return "", false
}

// parseReference resolves a full, collapsed or shortcut reference link against
// the definitions collected in phase 1.
func (p *inlineParser) parseReference(br *bracket, labelEnd int) (dest, title string, ok bool) {
	inner := p.s[br.after:labelEnd]

	// Full reference: [text][label]
	if p.pos < len(p.s) && p.s[p.pos] == '[' {
		if end := strings.IndexByte(p.s[p.pos:], ']'); end >= 0 {
			label := p.s[p.pos+1 : p.pos+end]
			if label != "" {
				if ref, found := p.refs[normalizeLabel(label)]; found {
					p.pos += end + 1
					return ref.dest, ref.title, true
				}
				return "", "", false
			}
			// Collapsed reference: [text][]
			if ref, found := p.refs[normalizeLabel(inner)]; found {
				p.pos += end + 1
				return ref.dest, ref.title, true
			}
			return "", "", false
		}
	}

	// Shortcut reference: [text]
	if ref, found := p.refs[normalizeLabel(inner)]; found {
		return ref.dest, ref.title, true
	}
	return "", "", false
}

func (p *inlineParser) skipInlineSpace() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n':
			p.pos++
		default:
			return
		}
	}
}

// unescapeBackslashes resolves backslash escapes and character references in
// destinations, titles and code-fence info strings, where the inline parser's
// normal handling does not run. The specification requires both: a destination
// written "/f&ouml;&ouml;" is the same URL as "/föö".
func unescapeBackslashes(s string) string {
	if !strings.ContainsRune(s, '&') && !strings.ContainsRune(s, '\\') {
		return s
	}
	s = resolveEntities(s)
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			i++
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// parseAngle handles everything that starts with "<": autolinks and raw HTML.
func (p *inlineParser) parseAngle() {
	if dest, label, ok := p.parseAutolink(); ok {
		p.append(&inode{
			kind:  InlineLink,
			dest:  dest,
			first: &inode{kind: InlineText, text: label},
		})
		return
	}
	if raw, ok := p.parseRawHTML(); ok {
		p.append(&inode{kind: InlineRawHTML, text: raw})
		return
	}
	p.pos++
	p.appendText("<")
}

// parseAutolink matches <scheme:...> and <user@host>. It returns the
// destination and the visible label separately, because an email autolink is
// rendered with a mailto: href but without mailto: in its text.
func (p *inlineParser) parseAutolink() (dest, label string, ok bool) {
	end := strings.IndexByte(p.s[p.pos:], '>')
	if end < 0 {
		return "", "", false
	}
	body := p.s[p.pos+1 : p.pos+end]
	if body == "" || strings.ContainsAny(body, " \t\n<") {
		return "", "", false
	}

	// A scheme is a letter followed by up to 31 letters, digits, +, . or -.
	// A scheme is two to thirty-two characters, so "<m:abc>" is not an autolink.
	if colon := strings.IndexByte(body, ':'); colon >= 2 && colon <= 32 && isAlpha(body[0]) {
		valid := true
		for i := 1; i < colon; i++ {
			c := body[i]
			if !isAlpha(c) && !isDigit(c) && c != '+' && c != '.' && c != '-' {
				valid = false
				break
			}
		}
		if valid {
			p.pos += end + 1
			return body, body, true
		}
	}

	// An email autolink gets a mailto: prefix.
	if isEmailAddress(body) {
		p.pos += end + 1
		return "mailto:" + body, body, true
	}
	return "", "", false
}

// isEmailAddress reports whether s matches the address syntax the specification
// defines for email autolinks.
//
// The characters allowed before the "@" are a fixed set, and a backslash is not
// among them, which is what keeps "<foo\\+@bar.example.com>" literal. Each
// dot-separated label after the "@" must begin and end alphanumerically and may
// otherwise contain hyphens.
func isEmailAddress(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	const localPunct = ".!#$%&'*+/=?^_`{|}~-"
	for i := 0; i < at; i++ {
		if c := s[i]; !isAlphaNum(c) && strings.IndexByte(localPunct, c) < 0 {
			return false
		}
	}
	for _, label := range strings.Split(s[at+1:], ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if !isAlphaNum(label[0]) || !isAlphaNum(label[len(label)-1]) {
			return false
		}
		for i := 1; i < len(label)-1; i++ {
			if !isAlphaNum(label[i]) && label[i] != '-' {
				return false
			}
		}
	}
	return true
}

func isAlphaNum(c byte) bool { return isAlpha(c) || isDigit(c) }

// parseRawHTML matches a tag, closing tag, comment, processing instruction,
// declaration or CDATA section, and passes it through untouched.
func (p *inlineParser) parseRawHTML() (string, bool) {
	s := p.s[p.pos:]
	for _, pair := range [][2]string{
		{"<!--", "-->"},
		{"<?", "?>"},
		{"<![CDATA[", "]]>"},
	} {
		if strings.HasPrefix(s, pair[0]) {
			if end := strings.Index(s, pair[1]); end >= 0 {
				raw := s[:end+len(pair[1])]
				p.pos += len(raw)
				return raw, true
			}
			return "", false
		}
	}
	if strings.HasPrefix(s, "<!") && len(s) > 2 && isAlpha(s[2]) {
		if end := strings.IndexByte(s, '>'); end >= 0 {
			raw := s[:end+1]
			p.pos += len(raw)
			return raw, true
		}
		return "", false
	}

	if n, ok := scanHTMLTag(s); ok {
		raw := s[:n]
		p.pos += n
		return raw, true
	}
	return "", false
}

// scanHTMLTag matches a complete open or closing tag at the start of s and
// returns its length.
//
// The syntax has to be checked properly rather than scanned to the next ">".
// An earlier version accepted anything of the form "<letters...>", which meant
// the autolink <https://example.com> parsed as a tag: the name "https" followed
// by ":" -- not whitespace, not "/", not ">" -- which a real tag cannot contain.
// The bug was invisible until HTML blocks started consulting the same function,
// at which point a line holding nothing but an autolink became a block of raw
// HTML and vanished from the output.
func scanHTMLTag(s string) (int, bool) {
	if len(s) < 3 || s[0] != '<' {
		return 0, false
	}
	i := 1
	closing := s[i] == '/'
	if closing {
		i++
	}

	// Tag name: a letter, then letters, digits and hyphens.
	if i >= len(s) || !isAlpha(s[i]) {
		return 0, false
	}
	for i++; i < len(s) && (isAlpha(s[i]) || isDigit(s[i]) || s[i] == '-'); i++ {
	}

	if closing {
		for i < len(s) && isHTMLSpace(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '>' {
			return i + 1, true
		}
		return 0, false
	}

	for {
		start := i
		for i < len(s) && isHTMLSpace(s[i]) {
			i++
		}
		spaced := i > start

		if i < len(s) && s[i] == '>' {
			return i + 1, true
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '>' {
			return i + 2, true
		}
		// Attributes must be separated from the name and from each other.
		if !spaced || i >= len(s) {
			return 0, false
		}

		if !isAlpha(s[i]) && s[i] != '_' && s[i] != ':' {
			return 0, false
		}
		for i++; i < len(s) && (isAlpha(s[i]) || isDigit(s[i]) ||
			s[i] == '_' || s[i] == '.' || s[i] == ':' || s[i] == '-'); i++ {
		}

		// An optional value, quoted or bare.
		j := i
		for j < len(s) && isHTMLSpace(s[j]) {
			j++
		}
		if j >= len(s) || s[j] != '=' {
			continue
		}
		j++
		for j < len(s) && isHTMLSpace(s[j]) {
			j++
		}
		if j >= len(s) {
			return 0, false
		}
		switch quote := s[j]; quote {
		case '\'', '"':
			j++
			for j < len(s) && s[j] != quote {
				j++
			}
			if j >= len(s) {
				return 0, false
			}
			j++
		default:
			valueStart := j
			for j < len(s) && !isHTMLSpace(s[j]) &&
				strings.IndexByte("\"'=<>`", s[j]) < 0 {
				j++
			}
			if j == valueStart {
				return 0, false
			}
		}
		i = j
	}
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
