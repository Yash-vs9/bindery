package main

import "testing"

// Inline expectations are written as rendered HTML rather than as trees, because
// that is the unit the CommonMark conformance suite compares and it keeps these
// tests and the spec runner honest about the same thing.
func TestInlineHTML(t *testing.T) {
	tests := []struct{ name, in, want string }{
		// Emphasis.
		{"emphasis", "*foo*", "<p><em>foo</em></p>\n"},
		{"strong", "**foo**", "<p><strong>foo</strong></p>\n"},
		{"strong inside emphasis", "***foo***", "<p><em><strong>foo</strong></em></p>\n"},
		{"nested pair", "*foo **bar** baz*", "<p><em>foo <strong>bar</strong> baz</em></p>\n"},
		{"adjacent runs", "*foo**bar**baz*", "<p><em>foo<strong>bar</strong>baz</em></p>\n"},
		{"underscore emphasis", "_foo_", "<p><em>foo</em></p>\n"},
		{"underscore inside word", "snake_case_name", "<p>snake_case_name</p>\n"},
		{"asterisk inside word", "foo*bar*", "<p>foo<em>bar</em></p>\n"},
		{"space after opener is literal", "a * foo bar*", "<p>a * foo bar*</p>\n"},
		{"unmatched delimiter", "*foo", "<p>*foo</p>\n"},
		{"triple unmatched", "***foo", "<p>***foo</p>\n"},

		// Code spans.
		{"code span", "`code`", "<p><code>code</code></p>\n"},
		{"code span holds backtick", "`` ` ``", "<p><code>`</code></p>\n"},
		{"code span keeps markup literal", "`*not emph*`", "<p><code>*not emph*</code></p>\n"},
		{"unclosed backtick is literal", "`foo", "<p>`foo</p>\n"},
		{"code span escapes html", "`<b>`", "<p><code>&lt;b&gt;</code></p>\n"},

		// Escapes and entities.
		{"backslash escape", `\*not emph\*`, "<p>*not emph*</p>\n"},
		{"backslash before letter is literal", `\a`, "<p>\\a</p>\n"},
		{"named entity", "&copy;", "<p>©</p>\n"},
		{"ampersand entity", "&amp;", "<p>&amp;</p>\n"},
		{"numeric entity", "&#35;", "<p>#</p>\n"},
		{"unknown entity stays literal", "&nonexistent;", "<p>&amp;nonexistent;</p>\n"},
		{"bare ampersand", "a & b", "<p>a &amp; b</p>\n"},
		{"bare less than", "4 < 5", "<p>4 &lt; 5</p>\n"},

		// Links and images.
		{"inline link", "[text](/url)", `<p><a href="/url">text</a></p>` + "\n"},
		{"link with title", `[text](/url "title")`, `<p><a href="/url" title="title">text</a></p>` + "\n"},
		{"angle destination", "[text](</url with space>)", `<p><a href="/url%20with%20space">text</a></p>` + "\n"},
		{"empty destination", "[text]()", `<p><a href="">text</a></p>` + "\n"},
		{"emphasis inside link", "[*text*](/url)", `<p><a href="/url"><em>text</em></a></p>` + "\n"},
		{"links do not nest", "[a [b](c)](d)", `<p>[a <a href="c">b</a>](d)</p>` + "\n"},
		{"image", "![alt](/img.png)", `<p><img src="/img.png" alt="alt" /></p>` + "\n"},
		{"image alt strips markup", "![*a* b](/i.png)", `<p><img src="/i.png" alt="a b" /></p>` + "\n"},
		{"undefined reference is literal", "[text][missing]", "<p>[text][missing]</p>\n"},
		{"bracket without link", "[not a link]", "<p>[not a link]</p>\n"},

		// Autolinks and raw HTML.
		{"uri autolink", "<https://example.com>", `<p><a href="https://example.com">https://example.com</a></p>` + "\n"},
		{"email autolink", "<foo@bar.com>", `<p><a href="mailto:foo@bar.com">foo@bar.com</a></p>` + "\n"},
		{"raw html passes through", "<span>x</span>", "<p><span>x</span></p>\n"},
		{"html comment", "a <!-- c --> b", "<p>a <!-- c --> b</p>\n"},

		// Breaks.
		{"soft break", "foo\nbar", "<p>foo\nbar</p>\n"},
		{"hard break from spaces", "foo  \nbar", "<p>foo<br />\nbar</p>\n"},
		{"hard break from backslash", "foo\\\nbar", "<p>foo<br />\nbar</p>\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderHTML(Parse(tt.in)); got != tt.want {
				t.Errorf("Parse(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

// FuzzParse covers both phases: arbitrary input must not panic, and must not
// hang, which the delimiter stack's openersBottom bookkeeping is what prevents.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"*a*", "**a**", "***a***", "[a](b)", "![a](b)", "`a`", "<a@b.c>",
		"a\\\nb", "&copy;", "[a [b](c)](d)", "*a**b**c*", "_a_b_c_",
		"``` ```", "********************", "[[[[[a]]]]]",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		RenderHTML(Parse(src))
	})
}
