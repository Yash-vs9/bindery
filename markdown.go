package main

import "strings"

// The document model.
//
// CommonMark is specified as two passes over the input, and this file holds the
// shape that connects them. Phase 1 (block.go) is line-oriented: it builds a
// tree of blocks and accumulates the raw text of each leaf without looking
// inside it. Phase 2 (inline.go) is character-oriented and turns that raw text
// into inline nodes. The passes cannot be fused, because a link reference
// definition may appear after the paragraph that refers to it, so inline
// resolution has to wait until every line has been seen.
//
// Blocks are one struct with a Kind tag rather than an interface hierarchy.
// Every consumer -- three renderers, the heading extractor, the search indexer
// -- switches on the kind anyway, and phase 1 mutates open blocks in place as
// it appends lines to them, which a tree of distinct types makes clumsier than
// it needs to be.

// BlockKind identifies a node in the block tree.
type BlockKind int

const (
	KindDocument BlockKind = iota
	KindParagraph
	KindHeading       // Level 1-6, from ATX (#) or setext (===) syntax
	KindThematicBreak // ---, ***, ___
	KindCodeFenced    // ``` or ~~~, Info holds the info string
	KindCodeIndented  // four spaces or one tab
	KindQuote         // >
	KindList          // container of KindListItem
	KindListItem
	KindHTMLBlock
)

var blockKindNames = [...]string{
	"Document", "Paragraph", "Heading", "ThematicBreak", "CodeFenced",
	"CodeIndented", "Quote", "List", "ListItem", "HTMLBlock",
}

func (k BlockKind) String() string {
	if int(k) < len(blockKindNames) {
		return blockKindNames[k]
	}
	return "Block(?)"
}

// Block is a node in the block tree.
//
// Container blocks (Document, Quote, List, ListItem) use Children. Leaf blocks
// use lines during phase 1 and Inlines after phase 2; code blocks keep their
// lines verbatim and never gain inlines, because their content is literal text.
type Block struct {
	Kind     BlockKind
	Line     int // 1-based source line where this block began, for error messages
	Children []*Block
	Inlines  []Inline

	Level int    // Heading: 1-6
	Info  string // CodeFenced: the info string after the opening fence

	// List and ListItem.
	Ordered  bool // List: 1. rather than -
	Start    int  // List: the number the first item carries
	Tight    bool // List: rendered without <p> wrappers around item content
	Marker   byte // List, ListItem: one of -+* or . )
	contIndl int  // ListItem: columns of indentation its content requires

	// Phase 1 scratch state, unexported because it is meaningless afterwards.
	// parent is unexported for a second reason: the tree would otherwise be
	// cyclic, and encoding/json follows exported fields.
	parent      *Block
	lines       []string
	open        bool
	fenceChar   byte
	fenceLen    int
	fenceIndent int
	htmlEnd     func(string) bool // HTMLBlock: reports whether a line closes it
	lastBlank   bool
	dropped     bool // a paragraph that held only link reference definitions
}

// Text returns the accumulated raw text of a leaf block. Code blocks keep their
// trailing newline; everything else is trimmed, because the spec strips leading
// and trailing whitespace from paragraph and heading content.
func (b *Block) Text() string {
	if b.Kind == KindCodeFenced || b.Kind == KindCodeIndented || b.Kind == KindHTMLBlock {
		if len(b.lines) == 0 {
			return ""
		}
		return strings.Join(b.lines, "\n") + "\n"
	}
	return strings.TrimSpace(strings.Join(b.lines, "\n"))
}

// isContainer reports whether a block holds other blocks rather than text.
func (b *Block) isContainer() bool {
	switch b.Kind {
	case KindDocument, KindQuote, KindList, KindListItem:
		return true
	}
	return false
}

// canContain reports whether a block of kind k may be a direct child of b.
// A list may only hold items, and an item may hold anything except a bare item.
func (b *Block) canContain(k BlockKind) bool {
	switch b.Kind {
	case KindList:
		return k == KindListItem
	case KindDocument, KindQuote, KindListItem:
		return k != KindListItem
	}
	return false
}

func (b *Block) lastChild() *Block {
	if len(b.Children) == 0 {
		return nil
	}
	return b.Children[len(b.Children)-1]
}

// InlineKind identifies a node in an inline sequence.
type InlineKind int

const (
	InlineText      InlineKind = iota
	InlineCode                 // `code`
	InlineEmph                 // *emphasis*
	InlineStrong               // **strong**
	InlineLink                 // [text](dest "title")
	InlineImage                // ![alt](dest "title")
	InlineRawHTML              // <span>
	InlineSoftBreak            // a plain newline
	InlineHardBreak            // two trailing spaces, or a trailing backslash
)

// Inline is a node in an inline sequence. Text carries the literal content of
// text, code and raw-HTML nodes; Children carries the content of emphasis and
// links, which nest.
type Inline struct {
	Kind     InlineKind
	Text     string
	Children []Inline
	Dest     string // Link, Image: the destination URL
	Title    string // Link, Image: the optional title
}

// linkRef is a link reference definition: [label]: dest "title", collected
// during phase 1 and consulted during phase 2.
type linkRef struct {
	dest  string
	title string
}

// Document is a parsed source file: the block tree plus the reference
// definitions that phase 2 needs to resolve shortcut and collapsed links.
type Document struct {
	Root *Block
	Refs map[string]linkRef
}

// MarshalText makes block kinds readable in JSON output. encoding/json and
// encoding/json/v2 both consult encoding.TextMarshaler, so implementing it here
// is enough for "bindery render --format=json" to emit names instead of the
// integers the constants really are.
func (k BlockKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

var inlineKindNames = [...]string{
	"Text", "Code", "Emph", "Strong", "Link", "Image", "RawHTML",
	"SoftBreak", "HardBreak",
}

func (k InlineKind) String() string {
	if int(k) < len(inlineKindNames) {
		return inlineKindNames[k]
	}
	return "Inline(?)"
}

func (k InlineKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }
