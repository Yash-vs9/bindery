package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Benchmarks.
//
// The corpus is every Markdown sample in the CommonMark suite concatenated:
// roughly 100KB of deliberately awkward input -- nested lists, reference
// definitions, HTML blocks, tab-indented containers, pathological emphasis. It
// is harder than real documentation, which is the point. A benchmark over
// friendly prose measures the fast path and nothing else.
//
// Numbers from these benchmarks are published in README.md with the machine
// they were measured on. They are absolute, not comparative: benchmarking
// against the parser this replaces would mean installing it, and the README
// says plainly what that means for the comparison.

func benchCorpus(tb testing.TB) string {
	tb.Helper()
	examples, err := loadSpec()
	if err != nil {
		tb.Fatal(err)
	}
	var sb strings.Builder
	for _, ex := range examples {
		sb.WriteString(ex.Markdown)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func BenchmarkParse(b *testing.B) {
	corpus := benchCorpus(b)
	b.SetBytes(int64(len(corpus)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Parse(corpus)
	}
}

func BenchmarkParseAndRenderHTML(b *testing.B) {
	corpus := benchCorpus(b)
	b.SetBytes(int64(len(corpus)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RenderHTML(Parse(corpus))
	}
}

func BenchmarkRenderHTMLOnly(b *testing.B) {
	corpus := benchCorpus(b)
	doc := Parse(corpus)
	b.SetBytes(int64(len(corpus)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RenderHTML(doc)
	}
}

// BenchmarkParseSmall measures a document the size of a real page, which is
// what latency in the dev server actually depends on.
func BenchmarkParseSmall(b *testing.B) {
	page, err := os.ReadFile(filepath.Join("..", "docs", "guide", "edge-cases.md"))
	if err != nil {
		b.Skip("docs corpus not available")
	}
	b.SetBytes(int64(len(page)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RenderHTML(Parse(string(page)))
	}
}

func BenchmarkHighlight(b *testing.B) {
	code := strings.Repeat("func main() {\n\ts := \"hi\" // note\n\tfor i := 0; i < 10; i++ {}\n}\n", 200)
	b.SetBytes(int64(len(code)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		highlightHTML(code, "go")
	}
}

func BenchmarkSearchIndex(b *testing.B) {
	site, err := LoadSite(filepath.Join("..", "docs"), false)
	if err != nil {
		b.Skip("docs corpus not available")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		BuildSearchIndex(site)
	}
}

// BenchmarkLoadSite measures the whole pipeline the dev server runs on every
// save: read, parse, render, extract headings.
func BenchmarkLoadSite(b *testing.B) {
	if _, err := os.Stat(filepath.Join("..", "docs")); err != nil {
		b.Skip("docs corpus not available")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := LoadSite(filepath.Join("..", "docs"), false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConformanceSuite(b *testing.B) {
	examples, err := loadSpec()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		runSpec(examples, "")
	}
}

// BenchmarkParseProse measures realistic documentation prose rather than the
// adversarial conformance corpus: this project's own README, STDLIB.md and
// docs. It is the input a documentation tool actually sees.
//
// The corpus is assembled from the real files at run time rather than committed
// as a fixture, so it cannot drift out of date with the documentation it is
// meant to represent.
func BenchmarkParseProse(b *testing.B) {
	var sb strings.Builder
	for _, name := range []string{
		"../README.md", "../STDLIB.md", "../docs/index.md",
		"../docs/guide/getting-started.md", "../docs/guide/edge-cases.md",
	} {
		part, err := os.ReadFile(name)
		if err != nil {
			b.Skip("prose corpus not available")
		}
		sb.Write(part)
		sb.WriteString("\n\n")
	}
	src := []byte(sb.String())
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RenderHTML(Parse(string(src)))
	}
}
