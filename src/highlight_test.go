package main

import (
	"strings"
	"testing"
)

func TestHighlight(t *testing.T) {
	tests := []struct {
		name, lang, code string
		wantContains     []string
		wantAbsent       []string
	}{
		{
			name: "go keywords and strings", lang: "go",
			code:         `func main() { s := "hi" // note` + "\n}",
			wantContains: []string{`<span class="hl-kw">func</span>`, `<span class="hl-str">&quot;hi&quot;</span>`, `<span class="hl-com">// note</span>`, `<span class="hl-fn">main</span>`},
		}, {
			name: "go raw string", lang: "go",
			code:         "s := " + "\x60raw \"quoted\" text\x60",
			wantContains: []string{`hl-str`},
		}, {
			name: "python triple quotes", lang: "python",
			code:         "def f():\n    \"\"\"doc\n    string\"\"\"\n    return 1",
			wantContains: []string{`<span class="hl-kw">def</span>`, `hl-str`, `<span class="hl-num">1</span>`},
		}, {
			name: "javascript template literal", lang: "js",
			code:         "const x = " + "\x60a ${b} c\x60" + ";",
			wantContains: []string{`<span class="hl-kw">const</span>`, `hl-str`},
		}, {
			name: "diff is line based", lang: "diff",
			code:         "@@ -1 +1 @@\n-old\n+new",
			wantContains: []string{`<span class="hl-del">-old</span>`, `<span class="hl-ins">+new</span>`},
		}, {
			name: "unknown language uses the generic lexer", lang: "brainfuck",
			code:         `x = "str" // c`,
			wantContains: []string{`hl-str`, `hl-com`},
		}, {
			name: "no language is left alone", lang: "",
			code:       `func main() {}`,
			wantAbsent: []string{"<span"},
		}, {
			name: "html in code is still escaped", lang: "go",
			code:         `x := "<b>&</b>"`,
			wantContains: []string{"&lt;b&gt;&amp;&lt;/b&gt;"},
			wantAbsent:   []string{"<b>"},
		}, {
			name: "unterminated string does not run away", lang: "go",
			code:         "s := \"unterminated\nnext := 1",
			wantContains: []string{"next"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := highlightHTML(tt.code, tt.lang)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\ngot: %s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("output should not contain %q\ngot: %s", absent, got)
				}
			}
		})
	}
}

// TestHighlightPreservesText is the invariant that matters: colouring must not
// change what the code says. Stripping the tags has to give the input back.
func TestHighlightPreservesText(t *testing.T) {
	samples := []struct{ lang, code string }{
		{"go", "package main\n\nfunc main() {\n\tprintln(\"hi\") // x\n}\n"},
		{"python", "class A:\n    def f(self):\n        return {'a': 1}\n"},
		{"js", "export const f = async (x) => `${x}`;\n"},
		{"json", "{\"a\": [1, 2.5, true, null]}\n"},
		{"sh", "for f in *.go; do echo \"$f\"; done\n"},
		{"diff", "--- a\n+++ b\n-x\n+y\n"},
		{"unknown", "anything at all # comment\n"},
	}
	for _, s := range samples {
		t.Run(s.lang, func(t *testing.T) {
			html := highlightHTML(s.code, s.lang)
			if got := stripTags(html); got != s.code {
				t.Errorf("highlighting changed the text\n got: %q\nwant: %q", got, s.code)
			}
		})
	}
}

// stripTags removes span tags and reverses HTML escaping.
func stripTags(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '<' {
			if end := strings.IndexByte(s[i:], '>'); end >= 0 {
				i += end + 1
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	r := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&amp;", "&")
	return r.Replace(sb.String())
}

func FuzzHighlight(f *testing.F) {
	for _, seed := range []string{
		`func f() { "s" }`, "/* unterminated", "'", `"`, "#", "0x1f", "a(", "--- x",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, code string) {
		for _, lang := range []string{"go", "python", "js", "json", "sh", "diff", "other"} {
			html := highlightHTML(code, lang)
			if got := stripTags(html); got != code {
				t.Fatalf("lang %s changed the text\n got: %q\nwant: %q", lang, got, code)
			}
		}
	})
}
