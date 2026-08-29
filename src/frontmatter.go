package main

import (
	"fmt"
	"strconv"
	"strings"
)

// YAML front matter.
//
// This is a deliberate subset, not an attempt at YAML. The full specification
// is large and contains genuinely surprising corners -- anchors, aliases, tags,
// merge keys, flow style, five kinds of block scalar, and the Norway problem,
// where the country code NO parses as the boolean false. Implementing all of it
// would be a bigger project than the Markdown parser and would serve nobody
// writing front matter.
//
// So the supported grammar is written down here and enforced, and anything
// outside it is a clear error with a line and column rather than a silent
// misreading:
//
//	block mappings          title: Getting started
//	nested mappings         by indentation
//	block sequences         - one
//	plain scalars           unquoted to end of line
//	quoted scalars          'single' and "double", with \ escapes in double
//	comments                # to end of line
//	types                   string, integer, float, boolean, null
//
// Not supported, and rejected rather than guessed at: anchors and aliases,
// tags, flow style ({} and []), block scalars (| and >), multiple documents,
// and complex keys.
//
// One deliberate deviation from YAML: an unquoted "no" is the string "no", not
// the boolean false. Only true and false are booleans here. YAML 1.1's larger
// set of boolean spellings is the single most common source of silent data
// corruption in configuration files, and front matter is not worth it.

// frontMatterError reports a problem with a position inside the front matter.
type frontMatterError struct {
	line, col int
	msg       string
}

func (e frontMatterError) Error() string {
	return fmt.Sprintf("front matter, line %d, column %d: %s", e.line, e.col, e.msg)
}

// splitFrontMatter separates a leading YAML block from the Markdown after it.
//
// Front matter must open on the very first line with "---" and close with
// "---" or "...". A document with no front matter is returned unchanged, with
// no error: front matter is optional, and a document beginning with a thematic
// break is not malformed.
func splitFrontMatter(src string) (values map[string]any, body string, err error) {
	src = normalize(src)
	if !strings.HasPrefix(src, "---\n") && src != "---" {
		return nil, src, nil
	}

	lines := strings.Split(src, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if trimmed := strings.TrimRight(lines[i], " \t"); trimmed == "---" || trimmed == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		// No closing delimiter. This is ambiguous: a document beginning "---"
		// may be one whose first element is a thematic break, or one whose
		// front matter someone forgot to close.
		//
		// The two are told apart by what follows. If the next content line
		// looks like a mapping entry, the author meant front matter and the
		// missing terminator is a mistake worth reporting with a position.
		// Otherwise it is ordinary Markdown and nothing is wrong.
		if looksLikeMapping(lines[1:]) {
			return nil, src, frontMatterError{
				line: 1, col: 1,
				msg: "front matter opened with --- but is never closed; expected a closing --- line",
			}
		}
		return nil, src, nil
	}

	p := &yamlParser{lines: lines[1:end]}
	values, err = p.parseMapping(0)
	if err != nil {
		return nil, strings.Join(lines[end+1:], "\n"), err
	}
	if p.i < len(p.lines) {
		return nil, strings.Join(lines[end+1:], "\n"), p.errorf(1, "unexpected indentation")
	}
	return values, strings.Join(lines[end+1:], "\n"), nil
}

type yamlParser struct {
	lines []string
	i     int
}

// errorf reports an error at the current line and the given column, both
// counted from the start of the document rather than the start of the block, so
// that the position matches what an editor shows.
func (p *yamlParser) errorf(col int, format string, args ...any) error {
	return frontMatterError{
		line: p.i + 2, // +1 for the opening --- and +1 to count from one
		col:  col,
		msg:  fmt.Sprintf(format, args...),
	}
}

// skipBlank advances past blank lines and comments.
func (p *yamlParser) skipBlank() {
	for p.i < len(p.lines) {
		trimmed := strings.TrimSpace(p.lines[p.i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return
		}
		p.i++
	}
}

// indentOf returns the number of leading spaces, and rejects tabs, which YAML
// forbids for indentation and which are a common invisible cause of confusion.
func (p *yamlParser) indentOf(line string) (int, error) {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	if n < len(line) && line[n] == '\t' {
		return 0, p.errorf(n+1, "tab used for indentation; YAML requires spaces")
	}
	return n, nil
}

// parseMapping reads key/value pairs at the given indentation.
func (p *yamlParser) parseMapping(indent int) (map[string]any, error) {
	result := map[string]any{}

	for {
		p.skipBlank()
		if p.i >= len(p.lines) {
			return result, nil
		}
		line := p.lines[p.i]
		got, err := p.indentOf(line)
		if err != nil {
			return nil, err
		}
		if got < indent {
			return result, nil
		}
		if got > indent {
			return nil, p.errorf(got+1, "unexpected indentation; expected %d spaces, found %d", indent, got)
		}

		rest := line[indent:]
		if strings.HasPrefix(rest, "- ") || rest == "-" {
			return nil, p.errorf(indent+1, "sequence item where a key was expected")
		}

		key, value, ok := splitKey(rest)
		if !ok {
			return nil, p.errorf(indent+1, "expected \"key: value\", found %q", strings.TrimSpace(rest))
		}
		if key == "" {
			return nil, p.errorf(indent+1, "empty key")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, p.errorf(indent+1, "duplicate key %q", key)
		}
		p.i++

		if strings.TrimSpace(value) != "" {
			scalar, err := p.scalar(strings.TrimSpace(value), indent+len(key)+2)
			if err != nil {
				return nil, err
			}
			result[key] = scalar
			continue
		}

		// An empty value means a nested block, or nothing at all.
		nested, err := p.parseNested(indent)
		if err != nil {
			return nil, err
		}
		result[key] = nested
	}
}

// parseNested reads the block belonging to a key with no inline value.
func (p *yamlParser) parseNested(parentIndent int) (any, error) {
	p.skipBlank()
	if p.i >= len(p.lines) {
		return nil, nil
	}
	got, err := p.indentOf(p.lines[p.i])
	if err != nil {
		return nil, err
	}
	if got <= parentIndent {
		// A key with nothing under it is a null value, which is how a front
		// matter field is left deliberately empty.
		if strings.HasPrefix(strings.TrimSpace(p.lines[p.i][got:]), "- ") {
			return p.parseSequence(got)
		}
		return nil, nil
	}
	if strings.HasPrefix(strings.TrimSpace(p.lines[p.i]), "- ") {
		return p.parseSequence(got)
	}
	return p.parseMapping(got)
}

// parseSequence reads a block sequence at the given indentation.
func (p *yamlParser) parseSequence(indent int) (any, error) {
	var items []any

	for {
		p.skipBlank()
		if p.i >= len(p.lines) {
			return items, nil
		}
		line := p.lines[p.i]
		got, err := p.indentOf(line)
		if err != nil {
			return nil, err
		}
		if got < indent {
			return items, nil
		}
		if got > indent {
			return nil, p.errorf(got+1, "unexpected indentation inside a sequence")
		}

		rest := line[indent:]
		if !strings.HasPrefix(rest, "- ") && rest != "-" {
			return items, nil
		}
		value := strings.TrimSpace(strings.TrimPrefix(rest, "-"))
		p.i++

		if value == "" {
			return nil, p.errorf(indent+1, "empty sequence item; nested blocks under \"-\" are not supported")
		}
		scalar, err := p.scalar(value, indent+3)
		if err != nil {
			return nil, err
		}
		items = append(items, scalar)
	}
}

// splitKey separates "key: value", ignoring colons inside quotes.
func splitKey(s string) (key, value string, ok bool) {
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if inSingle || inDouble {
				continue
			}
			// A colon only separates a key when whitespace or end of line
			// follows it, so that "url: https://example.com" keeps its scheme.
			if i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t' {
				return strings.TrimSpace(s[:i]), s[i+1:], true
			}
		}
	}
	return "", "", false
}

// scalar converts a scalar to a Go value.
func (p *yamlParser) scalar(s string, col int) (any, error) {
	if s == "" {
		return nil, nil
	}

	switch s[0] {
	case '\'':
		return unquoteSingle(s, p, col)
	case '"':
		return unquoteDouble(s, p, col)
	case '[', '{':
		return nil, p.errorf(col, "flow style is not supported; use a block sequence or mapping")
	case '|', '>':
		return nil, p.errorf(col, "block scalars (| and >) are not supported")
	case '&', '*', '!':
		return nil, p.errorf(col, "anchors, aliases and tags are not supported")
	}

	// Strip a trailing comment from a plain scalar.
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}

	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	return s, nil
}

func unquoteSingle(s string, p *yamlParser, col int) (any, error) {
	if len(s) < 2 || !strings.HasSuffix(s, "'") {
		return nil, p.errorf(col, "unterminated single-quoted string")
	}
	// In YAML a doubled single quote is a literal one; there are no backslash
	// escapes inside single quotes.
	return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
}

func unquoteDouble(s string, p *yamlParser, col int) (any, error) {
	if len(s) < 2 || !strings.HasSuffix(s, `"`) {
		return nil, p.errorf(col, "unterminated double-quoted string")
	}
	var sb strings.Builder
	body := s[1 : len(s)-1]
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' || i+1 >= len(body) {
			sb.WriteByte(body[i])
			continue
		}
		i++
		switch body[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case '0':
			sb.WriteByte(0)
		case '\\', '"', '/':
			sb.WriteByte(body[i])
		default:
			return nil, p.errorf(col+i, "unknown escape \\%c", body[i])
		}
	}
	return sb.String(), nil
}

// stringValue reads a string field from front matter, if present.
func stringValue(values map[string]any, key string) (string, bool) {
	if v, ok := values[key]; ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// boolValue reads a boolean field from front matter, defaulting when absent.
func boolValue(values map[string]any, key string, fallback bool) bool {
	if v, ok := values[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return fallback
}

// intValue reads an integer field from front matter, defaulting when absent.
func intValue(values map[string]any, key string, fallback int) int {
	if v, ok := values[key]; ok {
		if n, ok := v.(int64); ok {
			return int(n)
		}
	}
	return fallback
}

// looksLikeMapping reports whether the first content line reads as a YAML
// mapping entry. It is the heuristic that separates a forgotten front-matter
// terminator from a document that simply opens with a thematic break.
func looksLikeMapping(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := splitKey(trimmed)
		if !ok || key == "" {
			return false
		}
		// A key is a bare word. "Some sentence: with a colon" is prose.
		return strings.IndexFunc(key, func(r rune) bool {
			return r == ' ' || r == '\t'
		}) < 0
	}
	return false
}
