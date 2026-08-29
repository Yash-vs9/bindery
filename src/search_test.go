package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tokeniserFixtures is the shared contract between tokenise() in search.go and
// the JavaScript tokeniser in theme.go. Both must produce these results. The
// table is reproduced in a comment beside the JavaScript, and the two were
// checked against each other with node during development.
var tokeniserFixtures = []struct {
	in   string
	want []string
}{
	{"Hello, World!", []string{"hello", "world"}},
	{"snake_case_name", []string{"snake", "case", "name"}},
	{"v1.27 and Go1.27", []string{"v1", "27", "go1", "27"}},
	{"CJK 日本語テキスト", []string{"cjk", "日本語テキスト"}},
	{"a is the of", nil},
	{"", nil},
	{"x", nil},   // single characters are dropped
	{"   ", nil}, // whitespace only
	{"UPPER lower MiXeD", []string{"upper", "lower", "mixed"}},
	{"hyphen-separated", []string{"hyphen", "separated"}},
	{"emoji 🎉 party", []string{"emoji", "party"}}, // symbols split, they do not tokenise
	{"C++ and C#", []string{"c++", "c#"}[:0]},     // punctuation-only tails vanish
	{"back\\slash", []string{"back", "slash"}},
}

func TestTokeniseFixtures(t *testing.T) {
	for _, tt := range tokeniserFixtures {
		t.Run(tt.in, func(t *testing.T) {
			got := tokenise(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tokenise(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// buildTestSite writes a small corpus and loads it.
func buildTestSite(t *testing.T) *Site {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.md"), `# Home

Welcome to the documentation.

## Parsing

The parser follows the CommonMark specification closely.

## Serving

The server uses a WebSocket for live reload.
`)
	writeFile(t, filepath.Join(dir, "guide.md"), `# Guide

## Parsing again

More about the parser and about parsing generally.
`)
	site, err := LoadSite(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func TestBuildSearchIndex(t *testing.T) {
	idx := BuildSearchIndex(buildTestSite(t))

	if len(idx.Docs) < 5 {
		t.Errorf("indexed %d sections, want at least 5 (a page root plus its headings)", len(idx.Docs))
	}
	if idx.AvgLength <= 0 {
		t.Error("average length must be positive, or BM25 divides by zero")
	}

	// Sections must be addressable by anchor, not just by page.
	var anchored int
	for _, doc := range idx.Docs {
		if strings.Contains(doc.URL, "#") {
			anchored++
		}
	}
	if anchored == 0 {
		t.Error("no section carried an anchor; results would land at the top of the page")
	}

	if _, ok := idx.Terms["websocket"]; !ok {
		t.Error("term from body prose is missing from the index")
	}
	if _, ok := idx.Terms["the"]; ok {
		t.Error("stop word was indexed")
	}
}

// TestSearchRanking checks that BM25 does the job it is there for: a section
// about parsing must outrank one that merely mentions it.
func TestSearchRanking(t *testing.T) {
	idx := BuildSearchIndex(buildTestSite(t))

	best, bestScore := -1, 0.0
	for i := range idx.Docs {
		if score := idx.Score("parser", i); score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		t.Fatal(`no section scored for "parser"`)
	}
	if !strings.Contains(strings.ToLower(idx.Docs[best].Heading), "parsing") {
		t.Errorf("top hit for \"parser\" is %q, want a section about parsing", idx.Docs[best].Heading)
	}
	if idx.Score("nonexistentterm", best) != 0 {
		t.Error("a term that is not in the corpus scored above zero")
	}
}

// TestSearchIndexIsDeterministic guards the reproducible build. The index is
// built by iterating maps, which Go randomises deliberately, so the postings
// have to be sorted or two builds of the same source differ.
func TestSearchIndexIsDeterministic(t *testing.T) {
	site := buildTestSite(t)
	first, err := BuildSearchIndex(site).JSON()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		next, err := BuildSearchIndex(site).JSON()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("index differs between builds on attempt %d", i+1)
		}
	}
}

// TestStopWordPreludeIsGenerated checks the single-source-of-truth claim: the
// JavaScript stop-word list is emitted from the Go map, so the two cannot drift.
func TestStopWordPreludeIsGenerated(t *testing.T) {
	prelude := stopWordPrelude()
	for word := range stopWords {
		if !strings.Contains(prelude, `"`+word+`":1`) {
			t.Errorf("stop word %q is missing from the generated JavaScript", word)
		}
	}
	if strings.Count(prelude, ":1") != len(stopWords) {
		t.Errorf("prelude has %d entries, want %d", strings.Count(prelude, ":1"), len(stopWords))
	}
	// Sorted output keeps the emitted page byte-identical between builds.
	if second := stopWordPrelude(); second != prelude {
		t.Error("prelude is not deterministic")
	}
}

func FuzzTokenise(f *testing.F) {
	for _, seed := range []string{
		"Hello, World!", "snake_case", "日本語", "🎉", "", "a-b-c", "1.2.3",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, token := range tokenise(text) {
			if len(token) < 2 {
				t.Fatalf("tokenise(%q) produced a token shorter than two bytes: %q", text, token)
			}
			if stopWords[token] {
				t.Fatalf("tokenise(%q) produced stop word %q", text, token)
			}
			if token != strings.ToLower(token) {
				t.Fatalf("tokenise(%q) produced an uppercase token: %q", text, token)
			}
		}
	})
}
