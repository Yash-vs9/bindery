package main

import "strings"

// Syntax highlighting.
//
// This is what highlight.js, Prism and chroma are imported for. None of them is
// available here, and none is needed for the job a documentation site actually
// has: colouring keywords, strings, numbers and comments.
//
// Rather than three hand-written lexers that differ only in their tables, there
// is one scanner driven by a per-language description. Adding a language is
// then a matter of writing down its comment markers, string delimiters and
// keywords, not writing another state machine.
//
// The limits are real and worth stating: this is lexical, not grammatical.
// It has no parser, no symbol table and no idea what any identifier means. A
// word that is a keyword in one position is a keyword everywhere. That is the
// same trade every regex-based highlighter makes, and it is invisible at the
// size of a documentation code sample.

// class names, matching the CSS in theme.go.
const (
	classKeyword = "hl-kw"
	classString  = "hl-str"
	classNumber  = "hl-num"
	classComment = "hl-com"
	classType    = "hl-typ"
	classFunc    = "hl-fn"
	classInsert  = "hl-ins"
	classDelete  = "hl-del"
)

// span is a highlighted range of the source, in bytes.
type span struct {
	start, end int
	class      string
}

// stringSpec describes one kind of string literal.
type stringSpec struct {
	open, close string
	escape      bool // a backslash escapes the next character
	multiline   bool
}

// langSpec describes a language well enough to colour it.
type langSpec struct {
	lineComment  []string
	blockComment [2]string
	strings      []stringSpec
	keywords     map[string]bool
	types        map[string]bool
	lineBased    func(line string) string // for formats like diff
}

func words(list string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(list) {
		set[w] = true
	}
	return set
}

var (
	dquote = stringSpec{open: `"`, close: `"`, escape: true}
	squote = stringSpec{open: "'", close: "'", escape: true}

	goSpec = langSpec{
		lineComment:  []string{"//"},
		blockComment: [2]string{"/*", "*/"},
		strings: []stringSpec{
			dquote, squote,
			{open: "\x60", close: "\x60", multiline: true}, // raw string, backticks
		},
		keywords: words(`break case chan const continue default defer else
			fallthrough for func go goto if import interface map package range
			return select struct switch type var nil true false iota`),
		types: words(`bool byte complex64 complex128 error float32 float64 int
			int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64
			uintptr any comparable`),
	}

	jsSpec = langSpec{
		lineComment:  []string{"//"},
		blockComment: [2]string{"/*", "*/"},
		strings: []stringSpec{
			dquote, squote,
			{open: "\x60", close: "\x60", escape: true, multiline: true},
		},
		keywords: words(`async await break case catch class const continue
			debugger default delete do else export extends finally for from
			function get if implements import in instanceof interface let new
			of return set static super switch this throw try typeof var void
			while with yield null true false undefined`),
		types: words(`Array Boolean Date Error JSON Map Math Number Object
			Promise RegExp Set String Symbol WeakMap WeakSet any boolean never
			number string unknown void`),
	}

	pythonSpec = langSpec{
		lineComment: []string{"#"},
		strings: []stringSpec{
			{open: `"""`, close: `"""`, escape: true, multiline: true},
			{open: "'''", close: "'''", escape: true, multiline: true},
			dquote, squote,
		},
		keywords: words(`and as assert async await break class continue def del
			elif else except finally for from global if import in is lambda
			match nonlocal not or pass raise return try while with yield None
			True False`),
		types: words(`bool bytes dict float int list object set str tuple type`),
	}

	shellSpec = langSpec{
		lineComment: []string{"#"},
		strings:     []stringSpec{dquote, squote},
		keywords: words(`case do done elif else esac fi for function if in
			select then time until while cd echo exit export local read return
			set shift source unset`),
	}

	jsonSpec = langSpec{
		strings:  []stringSpec{dquote},
		keywords: words(`true false null`),
	}

	genericSpec = langSpec{
		lineComment:  []string{"//", "#", "--"},
		blockComment: [2]string{"/*", "*/"},
		strings:      []stringSpec{dquote, squote},
		keywords: words(`break case class const continue def default do else
			enum export for func function if import in let new package private
			public return static struct switch type var while true false null
			nil none`),
	}

	diffSpec = langSpec{lineBased: diffLineClass}
)

// languages maps info-string names, including common aliases, to descriptions.
var languages = map[string]*langSpec{
	"go": &goSpec, "golang": &goSpec,
	"js": &jsSpec, "javascript": &jsSpec, "ts": &jsSpec,
	"typescript": &jsSpec, "jsx": &jsSpec, "tsx": &jsSpec,
	"python": &pythonSpec, "py": &pythonSpec,
	"sh": &shellSpec, "bash": &shellSpec, "shell": &shellSpec, "zsh": &shellSpec,
	"json": &jsonSpec,
	"diff": &diffSpec, "patch": &diffSpec,
}

// specFor returns the description for a language name. An unknown but non-empty
// name gets the generic description, which is usually better than nothing: most
// languages agree about quotes and digits.
func specFor(lang string) *langSpec {
	if lang == "" {
		return nil
	}
	if s, ok := languages[strings.ToLower(lang)]; ok {
		return s
	}
	return &genericSpec
}

func diffLineClass(line string) string {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "@@"):
		return classComment
	case strings.HasPrefix(line, "+"):
		return classInsert
	case strings.HasPrefix(line, "-"):
		return classDelete
	}
	return ""
}

// highlightSpans scans src and returns the ranges to colour, in order and
// non-overlapping.
func highlightSpans(src, lang string) []span {
	spec := specFor(lang)
	if spec == nil {
		return nil
	}
	if spec.lineBased != nil {
		return lineBasedSpans(src, spec)
	}

	var spans []span
	i := 0
	for i < len(src) {
		if n, class, ok := scanToken(src, i, spec); ok {
			if class != "" {
				spans = append(spans, span{start: i, end: i + n, class: class})
			}
			i += n
			continue
		}
		i++
	}
	return spans
}

// scanToken matches one token at src[i:], returning its length and class.
func scanToken(src string, i int, spec *langSpec) (int, string, bool) {
	rest := src[i:]

	if spec.blockComment[0] != "" && strings.HasPrefix(rest, spec.blockComment[0]) {
		end := strings.Index(rest[len(spec.blockComment[0]):], spec.blockComment[1])
		if end < 0 {
			return len(rest), classComment, true // unterminated: to end of input
		}
		return len(spec.blockComment[0]) + end + len(spec.blockComment[1]), classComment, true
	}

	for _, marker := range spec.lineComment {
		if strings.HasPrefix(rest, marker) {
			if end := strings.IndexByte(rest, '\n'); end >= 0 {
				return end, classComment, true
			}
			return len(rest), classComment, true
		}
	}

	for _, str := range spec.strings {
		if strings.HasPrefix(rest, str.open) {
			return scanString(rest, str), classString, true
		}
	}

	if isDigit(src[i]) && (i == 0 || !isIdentChar(src[i-1])) {
		n := 1
		for i+n < len(src) && (isIdentChar(src[i+n]) || src[i+n] == '.') {
			n++
		}
		return n, classNumber, true
	}

	if isIdentStart(src[i]) {
		n := 1
		for i+n < len(src) && isIdentChar(src[i+n]) {
			n++
		}
		word := src[i : i+n]
		switch {
		case spec.keywords[word]:
			return n, classKeyword, true
		case spec.types[word]:
			return n, classType, true
		case followedByCall(src, i+n):
			return n, classFunc, true
		}
		return n, "", true // an ordinary identifier, scanned but not coloured
	}

	return 0, "", false
}

// scanString returns the length of a string literal beginning at s.
func scanString(s string, spec stringSpec) int {
	i := len(spec.open)
	for i < len(s) {
		switch {
		case spec.escape && s[i] == '\\' && i+1 < len(s):
			i += 2
			continue
		case !spec.multiline && s[i] == '\n':
			return i // an unterminated single-line string ends at the newline
		case strings.HasPrefix(s[i:], spec.close):
			return i + len(spec.close)
		}
		i++
	}
	return len(s)
}

// followedByCall reports whether an open parenthesis follows, which is the only
// evidence a lexer has that an identifier names a function.
func followedByCall(src string, i int) bool {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i < len(src) && src[i] == '('
}

func lineBasedSpans(src string, spec *langSpec) []span {
	var spans []span
	offset := 0
	for _, line := range strings.Split(src, "\n") {
		if class := spec.lineBased(line); class != "" {
			spans = append(spans, span{start: offset, end: offset + len(line), class: class})
		}
		offset += len(line) + 1
	}
	return spans
}

func isIdentStart(c byte) bool { return isAlpha(c) || c == '_' || c == '$' }
func isIdentChar(c byte) bool  { return isIdentStart(c) || isDigit(c) }

// highlightHTML renders code as escaped HTML with highlight spans applied.
func highlightHTML(src, lang string) string {
	spans := highlightSpans(src, lang)
	if len(spans) == 0 {
		return escapeHTML(src)
	}

	var sb strings.Builder
	sb.Grow(len(src) + 16*len(spans))
	last := 0
	for _, s := range spans {
		if s.start < last || s.end > len(src) {
			continue // defensive: never emit overlapping or out-of-range spans
		}
		sb.WriteString(escapeHTML(src[last:s.start]))
		sb.WriteString(`<span class="` + s.class + `">`)
		sb.WriteString(escapeHTML(src[s.start:s.end]))
		sb.WriteString("</span>")
		last = s.end
	}
	sb.WriteString(escapeHTML(src[last:]))
	return sb.String()
}
