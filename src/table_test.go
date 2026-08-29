package main

import "testing"

func parseTable(src string) *Document {
	return ParseWithOptions(src, ParseOptions{Tables: true})
}

func TestTableParsing(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "basic",
			in:   "a|b\n---|---\n1|2\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "leading and trailing pipes",
			in:   "| a | b |\n| --- | --- |\n| 1 | 2 |\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "alignment",
			in:   "a|b|c\n:--|:-:|--:\n1|2|3\n",
			want: `<table><thead><tr><th style="text-align:left">a</th>` +
				`<th style="text-align:center">b</th>` +
				`<th style="text-align:right">c</th></tr></thead>` +
				`<tbody><tr><td style="text-align:left">1</td>` +
				`<td style="text-align:center">2</td>` +
				`<td style="text-align:right">3</td></tr></tbody></table>` + "\n",
		}, {
			name: "header only, no body rows",
			in:   "a|b\n---|---\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead></table>\n",
		}, {
			name: "short row is padded with empty cells",
			in:   "a|b|c\n---|---|---\n1\n",
			want: "<table><thead><tr><th>a</th><th>b</th><th>c</th></tr></thead>" +
				"<tbody><tr><td>1</td><td></td><td></td></tr></tbody></table>\n",
		}, {
			name: "long row is truncated to the header width",
			in:   "a|b\n---|---\n1|2|3|4\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "blank line closes the table",
			in:   "a|b\n---|---\n1|2\n\nafter\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n<p>after</p>\n",
		}, {
			name: "escaped pipe stays inside the cell",
			in:   `a|b` + "\n---|---\n" + `1\|1|2` + "\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1|1</td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "pipe inside a code span is not a separator",
			in:   "a|b\n---|---\n`1|1`|2\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td><code>1|1</code></td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "cell content is inline-parsed",
			in:   "a|b\n---|---\n**bold**|`code`\n",
			want: "<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td><strong>bold</strong></td><td><code>code</code></td></tr></tbody></table>\n",
		}, {
			name: "one column requires a pipe on the delimiter row",
			in:   "a\n|---|\n1\n",
			want: "<table><thead><tr><th>a</th></tr></thead>" +
				"<tbody><tr><td>1</td></tr></tbody></table>\n",
		}, {
			name: "multi-line paragraph keeps its earlier lines as a paragraph",
			in:   "intro line\na|b\n---|---\n1|2\n",
			want: "<p>intro line</p>\n<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n",
		}, {
			name: "table in a blockquote",
			in:   "> a|b\n> ---|---\n> 1|2\n",
			want: "<blockquote>\n<table><thead><tr><th>a</th><th>b</th></tr></thead>" +
				"<tbody><tr><td>1</td><td>2</td></tr></tbody></table>\n</blockquote>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderHTML(parseTable(tt.in))
			if got != tt.want {
				t.Errorf("parse(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestTableRequiresDelimiterPipe is the regression test for the bug that would
// have broken every setext heading with a one-line paragraph above a bare "---"
// or "===": without requiring a literal pipe, "a\n---\n" would parse "---" as a
// valid one-column table delimiter row instead of a setext underline.
func TestTableRequiresDelimiterPipe(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"setext survives", "a\n---\n", "<h2>a</h2>\n"},
		{"setext level one survives", "a\n===\n", "<h1>a</h1>\n"},
		{"thematic break survives", "a\n\n---\n", "<p>a</p>\n<hr />\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderHTML(parseTable(tt.in)); got != tt.want {
				t.Errorf("parse(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestTablesAreOptional keeps the conformance number honest: bare Parse, which
// spec.go's conformance runner uses, must never recognise a table.
func TestTablesAreOptional(t *testing.T) {
	const src = "a|b\n---|---\n1|2\n"
	if got := RenderHTML(Parse(src)); got == "" || !contains(got, "<p>") {
		t.Errorf("bare Parse must render a table candidate as a paragraph, got %q", got)
	}
	if contains(RenderHTML(Parse(src)), "<table>") {
		t.Error("bare Parse recognised a table; conformance would no longer measure plain CommonMark")
	}
	if !contains(RenderHTML(parseTable(src)), "<table>") {
		t.Error("ParseWithOptions{Tables: true} did not recognise a table")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestTableANSI(t *testing.T) {
	doc := parseTable("Name|Age\n---|---:\nAda|36\n")
	out := RenderANSI(doc, 80, false)
	for _, want := range []string{"┌", "┬", "┐", "├", "┼", "┤", "└", "┴", "┘", "Name", "Age", "Ada", "36"} {
		if !contains(out, want) {
			t.Errorf("ANSI table output missing %q\n%s", want, out)
		}
	}
}

func TestTablePDF(t *testing.T) {
	doc := parseTable("Name|Age\n---|---:\nAda|36\n")
	pdf := string(RenderPDF(&Site{Pages: []*Page{{Title: "T", Doc: doc, URL: "/index.html"}}}, "T", false))
	for _, want := range []string{"Name", "Age", "Ada", "36", "re f"} { // "re f" = at least one filled rectangle was drawn
		if !contains(pdf, want) {
			t.Errorf("PDF table output missing %q", want)
		}
	}
}

func FuzzTableParsing(f *testing.F) {
	for _, seed := range []string{
		"a|b\n---|---\n1|2", "a\n---\n", "a\n===\n", "|---|\na\n",
		"a|b\n:-:|--:\n1|2", `a\|b` + "\n---\n" + "1|2",
		"a|b\n---|---\n`1|2`|3", "a|b|c\n---|---\n1", "", "a|b\n---\n1|2",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		RenderHTML(ParseWithOptions(src, ParseOptions{Tables: true}))
		RenderANSI(ParseWithOptions(src, ParseOptions{Tables: true}), 80, false)
	})
}
