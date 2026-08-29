package main

import (
	"fmt"
	"strings"
	"testing"
)

// dumpBlocks renders a block tree as a one-line s-expression so that structural
// expectations in tests read as structure rather than as prose.
func dumpBlocks(b *Block) string {
	var sb strings.Builder
	writeBlock(&sb, b)
	return sb.String()
}

func writeBlock(sb *strings.Builder, b *Block) {
	sb.WriteString("(")
	sb.WriteString(b.Kind.String())
	if b.Kind == KindHeading {
		fmt.Fprintf(sb, "%d", b.Level)
	}
	if b.Kind == KindCodeFenced && b.Info != "" {
		fmt.Fprintf(sb, ":%s", b.Info)
	}
	if !b.isContainer() {
		if text := b.Text(); text != "" {
			fmt.Fprintf(sb, " %q", text)
		}
	}
	for _, child := range b.Children {
		sb.WriteString(" ")
		writeBlock(sb, child)
	}
	sb.WriteString(")")
}

func TestParseBlocks(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{{
		name: "paragraph",
		in:   "hello world",
		want: `(Document (Paragraph "hello world"))`,
	}, {
		name: "paragraph spanning lines",
		in:   "one\ntwo",
		want: `(Document (Paragraph "one\ntwo"))`,
	}, {
		name: "blank line splits paragraphs",
		in:   "one\n\ntwo",
		want: `(Document (Paragraph "one") (Paragraph "two"))`,
	}, {
		name: "atx heading",
		in:   "## Title",
		want: `(Document (Heading2 "Title"))`,
	}, {
		name: "atx closing sequence stripped",
		in:   "## Title ##",
		want: `(Document (Heading2 "Title"))`,
	}, {
		name: "atx hash without space is a paragraph",
		in:   "#hashtag",
		want: `(Document (Paragraph "#hashtag"))`,
	}, {
		name: "seven hashes is a paragraph",
		in:   "####### too deep",
		want: `(Document (Paragraph "####### too deep"))`,
	}, {
		name: "setext heading beats thematic break",
		in:   "Title\n---",
		want: `(Document (Heading2 "Title"))`,
	}, {
		name: "thematic break with no paragraph above",
		in:   "---",
		want: `(Document (ThematicBreak))`,
	}, {
		name: "thematic break with spaces",
		in:   "* * *",
		want: `(Document (ThematicBreak))`,
	}, {
		name: "fenced code keeps markup literal",
		in:   "```\n# not a heading\n```",
		want: `(Document (CodeFenced "# not a heading\n"))`,
	}, {
		name: "fenced code info string",
		in:   "```go\nfunc main() {}\n```",
		want: `(Document (CodeFenced:go "func main() {}\n"))`,
	}, {
		name: "unclosed fence runs to end of document",
		in:   "```\nstill code",
		want: `(Document (CodeFenced "still code\n"))`,
	}, {
		name: "tilde fence may contain backticks",
		in:   "~~~\n```\n~~~",
		want: "(Document (CodeFenced \"```\\n\"))",
	}, {
		name: "indented code",
		in:   "    indented",
		want: `(Document (CodeIndented "indented\n"))`,
	}, {
		name: "tab indented code",
		in:   "\tindented",
		want: `(Document (CodeIndented "indented\n"))`,
	}, {
		name: "indented line after paragraph is continuation",
		in:   "text\n    more",
		want: `(Document (Paragraph "text\nmore"))`,
	}, {
		name: "blockquote",
		in:   "> quoted",
		want: `(Document (Quote (Paragraph "quoted")))`,
	}, {
		name: "lazy continuation inside quote",
		in:   "> one\ntwo",
		want: `(Document (Quote (Paragraph "one\ntwo")))`,
	}, {
		name: "quote ends at blank line",
		in:   "> one\n\ntwo",
		want: `(Document (Quote (Paragraph "one")) (Paragraph "two"))`,
	}, {
		name: "nested quotes",
		in:   "> > deep",
		want: `(Document (Quote (Quote (Paragraph "deep"))))`,
	}, {
		name: "heading inside quote",
		in:   "> # Title",
		want: `(Document (Quote (Heading1 "Title")))`,
	}, {
		name: "quote containing fence",
		in:   "> ```\n> code\n> ```",
		want: `(Document (Quote (CodeFenced "code\n")))`,
	}, {
		name: "crlf is normalised",
		in:   "one\r\ntwo\r\n",
		want: `(Document (Paragraph "one\ntwo"))`,
	}, {
		name: "nul becomes replacement character",
		in:   "a\x00b",
		want: `(Document (Paragraph "a�b"))`,
	}, {
		name: "empty document",
		in:   "",
		want: `(Document)`,
	}, {
		name: "only blank lines",
		in:   "\n\n\n",
		want: `(Document)`,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dumpBlocks(parseBlocks(tt.in).Root)
			if got != tt.want {
				t.Errorf("parseBlocks(%q)\n got: %s\nwant: %s", tt.in, got, tt.want)
			}
		})
	}
}

// FuzzParseBlocks asserts the only invariant that matters for arbitrary input:
// phase 1 terminates and does not panic. Go's native fuzzer replaces the
// property-testing package this would otherwise need.
func FuzzParseBlocks(f *testing.F) {
	for _, seed := range []string{
		"# h", "> q", "```\nx\n```", "    code", "---", "a\n\nb",
		"> > > x", "\t\t\t", "*  *  *", "#######", "~~~a`b~~~\nx",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		doc := parseBlocks(src)
		if doc.Root.Kind != KindDocument {
			t.Fatalf("root kind = %v, want Document", doc.Root.Kind)
		}
	})
}
