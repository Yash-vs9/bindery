package main

import "testing"

// The seven kinds of HTML block differ mainly in how they end, so most of these
// cases are about the end condition rather than the start.
func TestHTMLBlocks(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{
			"type 1 runs to its closing tag",
			"<pre>\n*not emphasis*\n</pre>",
			"<pre>\n*not emphasis*\n</pre>\n",
		}, {
			"type 1 script",
			"<script>\nvar x = 1 < 2;\n</script>",
			"<script>\nvar x = 1 < 2;\n</script>\n",
		}, {
			// A type 2 block ends at the line carrying "-->", and that line is
			// part of the block. What follows is ordinary content again.
			"type 2 comment ends at its closing line",
			"<!-- a\ncomment -->\ntext",
			"<!-- a\ncomment -->\n<p>text</p>\n",
		}, {
			"type 4 declaration",
			"<!DOCTYPE html>",
			"<!DOCTYPE html>\n",
		}, {
			"type 5 cdata",
			"<![CDATA[\nraw ]]>",
			"<![CDATA[\nraw ]]>\n",
		}, {
			"type 6 ends at a blank line",
			"<div>\n*not emphasis*\n</div>\n\n*emphasis*",
			"<div>\n*not emphasis*\n</div>\n<p><em>emphasis</em></p>\n",
		}, {
			"type 6 may interrupt a paragraph",
			"text\n<div>\nraw",
			"<p>text</p>\n<div>\nraw\n",
		}, {
			"type 7 may not interrupt a paragraph",
			"text\n<span>\nmore",
			"<p>text\n<span>\nmore</p>\n",
		}, {
			"type 7 alone starts a block",
			"<span>\nraw",
			"<span>\nraw\n",
		}, {
			"an autolink on its own line is not an HTML block",
			"<https://example.com>",
			`<p><a href="https://example.com">https://example.com</a></p>` + "\n",
		}, {
			"an email autolink on its own line is not an HTML block",
			"<foo@bar.com>",
			`<p><a href="mailto:foo@bar.com">foo@bar.com</a></p>` + "\n",
		}, {
			"an invalid tag is not an HTML block",
			"<33> text",
			"<p>&lt;33&gt; text</p>\n",
		}, {
			"tag with attributes",
			`<div class="x" id='y' data-n=3>`,
			`<div class="x" id='y' data-n=3>` + "\n",
		}, {
			"self closing tag",
			"<hr />",
			"<hr />\n",
		}, {
			"html block inside a blockquote",
			"> <div>\n> raw",
			"<blockquote>\n<div>\nraw\n</blockquote>\n",
		}, {
			"indented html keeps its indentation",
			"  <div>\n  raw",
			"  <div>\n  raw\n",
		}, {
			"four spaces makes it code, not html",
			"    <div>",
			"<pre><code>&lt;div&gt;\n</code></pre>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderHTML(Parse(tt.in)); got != tt.want {
				t.Errorf("Parse(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
