package main

import "testing"

// Link reference definitions are the construct that forces the parser to have
// two passes, so these cases matter out of proportion to how often the syntax
// is used.
func TestLinkReferenceDefinitions(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{
			"shortcut reference",
			"[foo]\n\n[foo]: /url",
			`<p><a href="/url">foo</a></p>` + "\n",
		}, {
			"definition may follow its use",
			"See [foo] for details.\n\n[foo]: /url \"Title\"",
			`<p>See <a href="/url" title="Title">foo</a> for details.</p>` + "\n",
		}, {
			"full reference",
			"[text][foo]\n\n[foo]: /url",
			`<p><a href="/url">text</a></p>` + "\n",
		}, {
			"collapsed reference",
			"[foo][]\n\n[foo]: /url",
			`<p><a href="/url">foo</a></p>` + "\n",
		}, {
			"labels are case insensitive",
			"[FOO]\n\n[foo]: /url",
			`<p><a href="/url">FOO</a></p>` + "\n",
		}, {
			"labels collapse internal whitespace",
			"[foo   bar]\n\n[foo bar]: /url",
			`<p><a href="/url">foo   bar</a></p>` + "\n",
		}, {
			"first definition wins",
			"[foo]\n\n[foo]: /first\n[foo]: /second",
			`<p><a href="/first">foo</a></p>` + "\n",
		}, {
			"definition may wrap onto the next line",
			"[foo]\n\n[foo]:\n/url\n\"Title\"",
			`<p><a href="/url" title="Title">foo</a></p>` + "\n",
		}, {
			"angle bracket destination",
			"[foo]\n\n[foo]: <a b>",
			`<p><a href="a%20b">foo</a></p>` + "\n",
		}, {
			"single quoted title",
			"[foo]\n\n[foo]: /url 'Title'",
			`<p><a href="/url" title="Title">foo</a></p>` + "\n",
		}, {
			"parenthesised title",
			"[foo]\n\n[foo]: /url (Title)",
			`<p><a href="/url" title="Title">foo</a></p>` + "\n",
		}, {
			"a document of only definitions renders empty",
			"[foo]: /url\n[bar]: /other",
			"",
		}, {
			"definitions before prose leave the prose",
			"[foo]: /url\nText after.",
			`<p>Text after.</p>` + "\n",
		}, {
			"undefined label stays literal",
			"[missing]\n\n[other]: /url",
			"<p>[missing]</p>\n",
		}, {
			"a definition inside a paragraph is not a definition",
			"Some text.\n[foo]: /url",
			"<p>Some text.\n[foo]: /url</p>\n",
		}, {
			"a title may sit on the line after the destination",
			"[foo]\n\n[foo]: /url\n\"Title\"",
			`<p><a href="/url" title="Title">foo</a></p>` + "\n",
		}, {
			// What invalidates a title is trailing content on its line: the
			// definition then ends at the destination, and the would-be title
			// becomes an ordinary paragraph.
			"trailing content makes it not a title",
			"[foo]\n\n[foo]: /url\n\"Title\" ok",
			`<p><a href="/url">foo</a></p>` + "\n<p>&quot;Title&quot; ok</p>\n",
		}, {
			"empty label is not a definition",
			"[]: /url\n\ntext",
			"<p>[]: /url</p>\n<p>text</p>\n",
		}, {
			"definition inside a blockquote",
			"> [foo]\n>\n> [foo]: /url",
			"<blockquote>\n" + `<p><a href="/url">foo</a></p>` + "\n</blockquote>\n",
		}, {
			"reference image",
			"![alt][foo]\n\n[foo]: /img.png",
			`<p><img src="/img.png" alt="alt" /></p>` + "\n",
		}, {
			"indented four spaces is code, not a definition",
			"    [foo]: /url",
			"<pre><code>[foo]: /url\n</code></pre>\n",
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
