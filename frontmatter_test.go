package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitFrontMatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]any
		body string
	}{
		{
			name: "no front matter",
			in:   "# Title\n",
			want: nil,
			body: "# Title\n",
		}, {
			name: "simple mapping",
			in:   "---\ntitle: Getting started\norder: 2\n---\n# Title\n",
			want: map[string]any{"title": "Getting started", "order": int64(2)},
			body: "# Title\n",
		}, {
			name: "types",
			in:   "---\ns: text\ni: 42\nf: 1.5\nt: true\nf2: false\nn: null\ne: ~\n---\n",
			want: map[string]any{
				"s": "text", "i": int64(42), "f": 1.5,
				"t": true, "f2": false, "n": nil, "e": nil,
			},
			body: "",
		}, {
			name: "quoted scalars",
			in:   "---\na: 'it''s here'\nb: \"line\\nbreak\"\nc: \"42\"\n---\n",
			want: map[string]any{"a": "it's here", "b": "line\nbreak", "c": "42"},
			body: "",
		}, {
			name: "a url keeps its scheme",
			in:   "---\nurl: https://example.com/x\n---\n",
			want: map[string]any{"url": "https://example.com/x"},
			body: "",
		}, {
			name: "sequence",
			in:   "---\ntags:\n  - go\n  - parsers\n---\n",
			want: map[string]any{"tags": []any{"go", "parsers"}},
			body: "",
		}, {
			name: "nested mapping",
			in:   "---\nauthor:\n  name: Ada\n  year: 1843\n---\n",
			want: map[string]any{"author": map[string]any{"name": "Ada", "year": int64(1843)}},
			body: "",
		}, {
			name: "comments and blank lines",
			in:   "---\n# a comment\n\ntitle: X   # trailing\n---\n",
			want: map[string]any{"title": "X"},
			body: "",
		}, {
			name: "empty value is null",
			in:   "---\ntitle:\n---\n",
			want: map[string]any{"title": nil},
			body: "",
		}, {
			name: "closing dots",
			in:   "---\na: 1\n...\nbody\n",
			want: map[string]any{"a": int64(1)},
			body: "body\n",
		}, {
			// YAML 1.1 reads an unquoted "no" as false, which silently corrupts
			// country codes and answers alike. bindery reads only true and false.
			name: "no is a string, not a boolean",
			in:   "---\ncountry: no\nanswer: NO\n---\n",
			want: map[string]any{"country": "no", "answer": "NO"},
			body: "",
		}, {
			name: "a thematic break is not front matter",
			in:   "---\n",
			want: nil,
			body: "---\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, body, err := splitFrontMatter(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("values = %#v, want %#v", got, tt.want)
			}
			if body != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
		})
	}
}

// Errors carry a position and say what was wrong, because a front-matter
// mistake is otherwise invisible: the page just renders without its title.
func TestFrontMatterErrors(t *testing.T) {
	tests := []struct {
		name, in, wantSubstring string
	}{
		{"unterminated", "---\ntitle: X\n", "never closed"},
		{"not a mapping", "---\njust text\n---\n", `expected "key: value"`},
		{"tab indentation", "---\na:\n\tb: 1\n---\n", "tab used for indentation"},
		{"flow style", "---\na: [1, 2]\n---\n", "flow style is not supported"},
		{"block scalar", "---\na: |\n---\n", "block scalars"},
		{"anchor", "---\na: &anchor\n---\n", "anchors, aliases and tags"},
		{"duplicate key", "---\na: 1\na: 2\n---\n", "duplicate key"},
		{"unterminated quote", "---\na: 'x\n---\n", "unterminated single-quoted"},
		{"bad escape", `---` + "\n" + `a: "x\qy"` + "\n---\n", "unknown escape"},
		{"unexpected indent", "---\na: 1\n   b: 2\n---\n", "unexpected indentation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := splitFrontMatter(tt.in)
			if err == nil {
				t.Fatalf("expected an error for %q", tt.in)
			}
			if !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantSubstring)
			}
			if !strings.Contains(err.Error(), "line") {
				t.Errorf("error = %q, want it to carry a line number", err)
			}
		})
	}
}

func FuzzSplitFrontMatter(f *testing.F) {
	for _, seed := range []string{
		"---\na: 1\n---\nbody", "---\n", "---\na:\n  - x\n---",
		"---\na: 'x''y'\n---", "---\n\t\n---", "---\na: \"\\n\"\n---",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		// Malformed input must produce an error, never a panic and never a
		// silent misreading.
		_, _, _ = splitFrontMatter(src)
	})
}
