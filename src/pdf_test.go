package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// pdfFixture builds a small site and renders it.
func pdfFixture(t *testing.T, source string) []byte {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.md"), source)
	site, err := LoadSite(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	return RenderPDF(site, "Title", true)
}

// TestPDFStructure checks the file a reader has to be able to open.
func TestPDFStructure(t *testing.T) {
	pdf := pdfFixture(t, "# Heading\n\nBody text.\n")

	if !bytes.HasPrefix(pdf, []byte("%PDF-1.7\n")) {
		t.Error("missing PDF header")
	}
	if !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
		t.Error("missing EOF marker")
	}
	for _, want := range []string{"/Type /Catalog", "/Type /Pages", "/Type /Page ", "xref", "trailer", "/Root 1 0 R", "startxref"} {
		if !bytes.Contains(pdf, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
}

// TestPDFCrossReferenceTable is the test that matters most.
//
// The cross-reference table gives the byte offset of every object. A reader
// seeks to those offsets directly, so an offset that is wrong by one byte makes
// the file unopenable while every other structural check still passes. This
// walks the table and confirms each entry lands exactly on "N 0 obj".
func TestPDFCrossReferenceTable(t *testing.T) {
	pdf := pdfFixture(t, "# H\n\nText with a [link](https://example.com).\n\n- item\n\n```\ncode\n```\n")
	text := string(pdf)

	startIdx := strings.LastIndex(text, "startxref\n")
	if startIdx < 0 {
		t.Fatal("no startxref")
	}
	var xrefOffset int
	if _, err := fmt.Sscanf(text[startIdx+len("startxref\n"):], "%d", &xrefOffset); err != nil {
		t.Fatalf("unreadable startxref: %v", err)
	}
	if xrefOffset <= 0 || xrefOffset >= len(text) {
		t.Fatalf("startxref points outside the file: %d of %d bytes", xrefOffset, len(text))
	}
	if !strings.HasPrefix(text[xrefOffset:], "xref\n") {
		t.Fatalf("startxref does not point at the table, found %q", text[xrefOffset:xrefOffset+16])
	}

	var count int
	if _, err := fmt.Sscanf(text[xrefOffset:], "xref\n0 %d", &count); err != nil {
		t.Fatalf("unreadable xref header: %v", err)
	}

	// Entries are fixed-width: ten digits, a space, five digits, a space, a
	// type byte, a space. Readers depend on exactly twenty bytes per entry.
	entries := text[strings.Index(text[xrefOffset:], "\n0000000000")+xrefOffset+1:]
	for n := 1; n < count; n++ {
		entry := entries[n*20 : n*20+20]
		if len(entry) != 20 || entry[10] != ' ' || entry[17] != 'n' {
			t.Fatalf("object %d has a malformed xref entry %q", n, entry)
		}
		offset, err := strconv.Atoi(strings.TrimLeft(entry[:10], "0") + "")
		if err != nil {
			t.Fatalf("object %d has an unreadable offset %q", n, entry[:10])
		}
		want := fmt.Sprintf("%d 0 obj", n)
		if !strings.HasPrefix(text[offset:], want) {
			t.Errorf("xref offset for object %d points at %q, want %q",
				n, text[offset:min(offset+20, len(text))], want)
		}
	}
}

// TestPDFPageCount checks that the page tree agrees with the pages present.
func TestPDFPageCount(t *testing.T) {
	pdf := string(pdfFixture(t, "# One\n\nText.\n"))

	var declared int
	if _, err := fmt.Sscanf(pdf[strings.Index(pdf, "/Count "):], "/Count %d", &declared); err != nil {
		t.Fatal(err)
	}
	actual := strings.Count(pdf, "/Type /Page ")
	if declared != actual {
		t.Errorf("page tree declares %d pages, %d page objects exist", declared, actual)
	}
	kids := regexp.MustCompile(`/Kids \[([^\]]*)\]`).FindStringSubmatch(pdf)
	if kids == nil {
		t.Fatal("no /Kids array")
	}
	if got := len(strings.Fields(kids[1])) / 3; got != declared {
		t.Errorf("/Kids lists %d pages, /Count says %d", got, declared)
	}
}

// TestPDFEscaping checks that content cannot break out of a PDF string.
// Parentheses delimit strings and a backslash escapes; unescaped, either would
// corrupt every object after it.
func TestPDFEscaping(t *testing.T) {
	pdf := string(pdfFixture(t, `# T

Text with (parentheses) and a \ backslash and )unbalanced.
`))
	for _, forbidden := range []string{"(Text with (parentheses)", `\ backslash`} {
		if strings.Contains(pdf, forbidden) {
			t.Errorf("unescaped sequence %q reached the content stream", forbidden)
		}
	}
	if !strings.Contains(pdf, `\(parentheses\)`) {
		t.Error("parentheses were not escaped")
	}
}

// TestPDFNonASCII documents the base-14 limitation rather than hiding it.
func TestPDFNonASCII(t *testing.T) {
	pdf := string(pdfFixture(t, "# T\n\nSmart “quotes”, an em—dash, ellipsis… and 日本語.\n"))

	if strings.Contains(pdf, "“") || strings.Contains(pdf, "日") {
		t.Error("non-ASCII reached the content stream; base-14 fonts cannot show it")
	}
	for _, want := range []string{`\"quotes\"`, "em-dash", "ellipsis..."} {
		if !strings.Contains(pdf, strings.ReplaceAll(want, `\`, "")) {
			t.Errorf("expected %q after transliteration", want)
		}
	}
	if !strings.Contains(pdf, "???") {
		t.Error("unrepresentable characters should become question marks, visibly")
	}
}

// TestPDFLinksAreAbsoluteOnly checks that relative links do not become dead
// clickable regions.
func TestPDFLinksAreAbsoluteOnly(t *testing.T) {
	pdf := string(pdfFixture(t, `# T

[external](https://example.com), [relative](guide/x.md), [anchor](#section).
`))
	if !strings.Contains(pdf, "/URI (https://example.com)") {
		t.Error("absolute link did not become an annotation")
	}
	for _, dead := range []string{"guide/x.md", "#section"} {
		if strings.Contains(pdf, "/URI ("+dead) {
			t.Errorf("relative target %q became a clickable link", dead)
		}
	}
}

// TestPDFIsDeterministic guards the reproducible-build claim. A /Info
// dictionary with a creation date is the usual reason PDFs differ between runs,
// which is why this writer emits none.
func TestPDFIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.md"), "# T\n\nBody.\n\n- a\n- b\n")
	site, err := LoadSite(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	first := RenderPDF(site, "Title", true)
	for i := 0; i < 5; i++ {
		if !bytes.Equal(first, RenderPDF(site, "Title", true)) {
			t.Fatalf("PDF output differs between renders on attempt %d", i+2)
		}
	}
	if bytes.Contains(first, []byte("/CreationDate")) {
		t.Error("a creation date would make the output non-reproducible")
	}
}

// TestPDFPagination checks that long input produces more than one page and that
// every page carries the shared resource dictionary.
func TestPDFPagination(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Long\n\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "Paragraph %d with enough words in it to occupy a line of the page.\n\n", i)
	}
	pdf := string(pdfFixture(t, sb.String()))

	pages := strings.Count(pdf, "/Type /Page ")
	if pages < 3 {
		t.Errorf("200 paragraphs produced %d pages, want at least 3", pages)
	}
	if got := strings.Count(pdf, "/F4 6 0 R"); got != pages {
		t.Errorf("%d pages but %d resource dictionaries", pages, got)
	}
}

func TestPDFFontMetrics(t *testing.T) {
	// Courier is monospaced; Helvetica is not. If these ever agree, the wrong
	// table is being consulted.
	if fontMono.measure("iii", 10) != fontMono.measure("MMM", 10) {
		t.Error("Courier is not monospaced in the metrics table")
	}
	if fontRegular.measure("iii", 10) == fontRegular.measure("MMM", 10) {
		t.Error("Helvetica is being measured as monospaced")
	}
	// A known value, so a corrupted table is caught rather than merely a
	// differently-shaped one: Helvetica 'M' is 833/1000 em.
	if got := fontRegular.measure("M", 1000); got != 833 {
		t.Errorf("Helvetica M = %v, want 833 (Adobe base-14 metrics)", got)
	}
	if got := fontMono.measure("M", 1000); got != 600 {
		t.Errorf("Courier M = %v, want 600", got)
	}
}

func FuzzRenderPDF(f *testing.F) {
	for _, seed := range []string{
		"# h\n\ntext", "- a\n- b", "```\ncode\n```", "> q", "[a](https://b.c)",
		"(parens)", `back\slash`, "日本語", strings.Repeat("word ", 400),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		doc := Parse(src)
		site := &Site{Pages: []*Page{{Title: "T", Doc: doc, URL: "/index.html"}}}
		pdf := RenderPDF(site, "T", false)

		// Whatever the input, the output must be a structurally complete file.
		if !bytes.HasPrefix(pdf, []byte("%PDF-1.7\n")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
			t.Fatalf("malformed PDF for input %q", src)
		}
		if !bytes.Contains(pdf, []byte("startxref")) {
			t.Fatalf("no cross-reference table for input %q", src)
		}
	})
}
