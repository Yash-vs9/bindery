package main

import "testing"

// Lists are the most intricate block construct in the specification: the
// container's continuation depends on a content indent derived from the marker,
// and the rendered shape depends on whether blank lines make the list "loose".
// These cases are the ones that distinguish an implementation that works from
// one that works on the author's own README.
func TestListHTML(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{
			"single bullet",
			"- foo",
			"<ul>\n<li>foo</li>\n</ul>\n",
		}, {
			"tight list",
			"- foo\n- bar",
			"<ul>\n<li>foo</li>\n<li>bar</li>\n</ul>\n",
		}, {
			"loose list wraps items in paragraphs",
			"- foo\n\n- bar",
			"<ul>\n<li>\n<p>foo</p>\n</li>\n<li>\n<p>bar</p>\n</li>\n</ul>\n",
		}, {
			"all three bullet characters work",
			"+ foo",
			"<ul>\n<li>foo</li>\n</ul>\n",
		}, {
			"changing the marker starts a new list",
			"- a\n* b",
			"<ul>\n<li>a</li>\n</ul>\n<ul>\n<li>b</li>\n</ul>\n",
		}, {
			"ordered list",
			"1. foo\n2. bar",
			"<ol>\n<li>foo</li>\n<li>bar</li>\n</ol>\n",
		}, {
			"ordered list with a start number",
			"3. foo",
			"<ol start=\"3\">\n<li>foo</li>\n</ol>\n",
		}, {
			"changing the delimiter starts a new list",
			"1. a\n1) b",
			"<ol>\n<li>a</li>\n</ol>\n<ol>\n<li>b</li>\n</ol>\n",
		}, {
			"nested list",
			"- a\n  - b",
			"<ul>\n<li>a\n<ul>\n<li>b</li>\n</ul>\n</li>\n</ul>\n",
		}, {
			"item with two paragraphs is loose",
			"- foo\n\n  bar",
			"<ul>\n<li>\n<p>foo</p>\n<p>bar</p>\n</li>\n</ul>\n",
		}, {
			"item containing a blockquote",
			"- > quoted",
			"<ul>\n<li>\n<blockquote>\n<p>quoted</p>\n</blockquote>\n</li>\n</ul>\n",
		}, {
			"item containing indented code",
			"- foo\n\n      bar",
			"<ul>\n<li>\n<p>foo</p>\n<pre><code>bar\n</code></pre>\n</li>\n</ul>\n",
		}, {
			"bullet list interrupts a paragraph",
			"foo\n- bar",
			"<p>foo</p>\n<ul>\n<li>bar</li>\n</ul>\n",
		}, {
			"ordered list not starting at one cannot interrupt a paragraph",
			"foo\n2. bar",
			"<p>foo\n2. bar</p>\n",
		}, {
			"ordered list starting at one may interrupt a paragraph",
			"foo\n1. bar",
			"<p>foo</p>\n<ol>\n<li>bar</li>\n</ol>\n",
		}, {
			"three hyphens are a thematic break, not a list",
			"- - -",
			"<hr />\n",
		}, {
			"marker with no space is not a list",
			"-foo",
			"<p>-foo</p>\n",
		}, {
			"continuation line indented to content",
			"- foo\n  bar",
			"<ul>\n<li>foo\nbar</li>\n</ul>\n",
		}, {
			"lazy continuation inside an item",
			"- foo\nbar",
			"<ul>\n<li>foo\nbar</li>\n</ul>\n",
		}, {
			"empty item",
			"-\n- foo",
			"<ul>\n<li></li>\n<li>foo</li>\n</ul>\n",
		}, {
			"list ends at an unindented paragraph",
			"- foo\n\nbar",
			"<ul>\n<li>foo</li>\n</ul>\n<p>bar</p>\n",
		}, {
			"heading inside an item",
			"- # Title",
			"<ul>\n<li>\n<h1>Title</h1>\n</li>\n</ul>\n",
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
