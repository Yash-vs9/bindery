package main

import (
	"strings"
	"testing"
)

// specBaseline is the number of CommonMark examples that must pass.
//
// It is a ratchet, not a target. The test fails if the score drops, which turns
// every future change into a conformance regression check for free; when the
// score improves, this number is raised deliberately. Pinning the exact figure
// rather than a percentage keeps it honest about single-example regressions.
const specBaseline = 651

func TestCommonMarkSpec(t *testing.T) {
	examples, err := loadSpec()
	if err != nil {
		t.Fatal(err)
	}
	result := runSpec(examples, "")

	t.Logf("CommonMark %s: %d/%d (%.1f%%)",
		specVersion, result.Passed, result.Total, result.Rate())

	if result.Passed < specBaseline {
		t.Errorf("conformance regressed: %d/%d passing, baseline is %d",
			result.Passed, result.Total, specBaseline)
		for _, f := range result.Failures {
			t.Logf("  example %d (%s): %q\n    want %q\n    got  %q",
				f.Example, f.Section, f.Markdown, f.Want, f.Got)
		}
	}
	if result.Passed > specBaseline {
		t.Errorf("conformance improved to %d/%d; raise specBaseline to %d",
			result.Passed, result.Total, result.Passed)
	}
}

// TestCommonMarkSpecBySection runs each section as a subtest, so that a failure
// says which part of the specification broke rather than only that something
// did. Sections at the baseline are reported, not failed.
func TestCommonMarkSpecBySection(t *testing.T) {
	examples, err := loadSpec()
	if err != nil {
		t.Fatal(err)
	}
	result := runSpec(examples, "")

	shortfall := map[string]int{}
	for _, f := range result.Failures {
		shortfall[f.Section]++
	}
	for _, s := range result.Sections {
		t.Run(strings.ReplaceAll(s.Name, " ", "_"), func(t *testing.T) {
			if n := shortfall[s.Name]; n > 0 {
				t.Logf("%d/%d passing, %d known failure%s", s.Passed, s.Total, n, plural(n))
			}
		})
	}
}

// TestUnicodeCaseFoldingIsSimpleNotFull documents the one failing example.
//
// CommonMark normalises link labels with full Unicode case folding, under which
// U+1E9E (capital sharp s) folds to the two characters "ss", so "[ẞ]" matches a
// definition of "[SS]". Go's standard library has only simple case mapping:
// strings.ToLower gives "ß" and strings.EqualFold does simple folding, neither
// of which can map one rune to two. golang.org/x/text has the full tables and is
// exactly the kind of dependency this project does not take.
//
// This asserts the gap rather than working around it. Special-casing sharp s to
// pass one example would be tuning to the test suite instead of implementing the
// specification, and would make the reported score mean less.
func TestUnicodeCaseFoldingIsSimpleNotFull(t *testing.T) {
	const capitalSharpS = "ẞ"

	if got := strings.ToLower(capitalSharpS); got != "ß" {
		t.Errorf("strings.ToLower(%q) = %q; the standard library's behaviour changed",
			capitalSharpS, got)
	}
	if strings.EqualFold(capitalSharpS, "SS") {
		t.Error("strings.EqualFold now does full case folding; the label " +
			"normaliser in normalizeLabel can be improved and specBaseline raised")
	}

	// The consequence, stated as a test so that it is visible rather than
	// buried in a document.
	got := RenderHTML(Parse("[ẞ]\n\n[SS]: /url\n"))
	want := "<p>[ẞ]</p>\n"
	if got != want {
		t.Errorf("case folding behaviour changed: got %q, want %q", got, want)
	}
}

// BenchmarkParseSpec measures throughput over the whole suite, which is a
// reasonable stand-in for mixed real-world Markdown.
func BenchmarkParseSpec(b *testing.B) {
	examples, err := loadSpec()
	if err != nil {
		b.Fatal(err)
	}
	var corpus strings.Builder
	for _, e := range examples {
		corpus.WriteString(e.Markdown)
		corpus.WriteString("\n\n")
	}
	src := corpus.String()

	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		RenderHTML(Parse(src))
	}
}

func BenchmarkParseREADME(b *testing.B) {
	src := strings.Repeat("# Heading\n\nA paragraph with *emphasis*, `code`, and a "+
		"[link](https://example.com).\n\n- item one\n- item two\n\n```go\nfunc main() {}\n```\n\n", 50)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		RenderHTML(Parse(src))
	}
}
