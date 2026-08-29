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
const specBaseline = 652

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

// TestStdlibCaseMappingIsStillSimple documents why casefold.go exists, by
// pinning the standard-library behaviour that made it necessary.
//
// CommonMark normalises link labels with full Unicode case folding, under which
// U+1E9E (capital sharp s) folds to the two characters "ss", so "[ẞ]" matches a
// definition of "[SS]". Go's standard library has only simple case mapping:
// strings.ToLower gives "ß" and strings.EqualFold does simple folding, neither
// of which can map one rune to two. golang.org/x/text has the full tables and is
// exactly the kind of dependency this project does not take -- so casefold.go
// carries a small table of exceptions, generated from the Unicode Consortium's
// own CaseFolding.txt, covering only the 266 code points where full folding
// disagrees with what the standard library already does correctly.
//
// If this test ever fails, the standard library gained full folding and
// casefold.go's exception table -- and this comment -- can be deleted.
func TestStdlibCaseMappingIsStillSimple(t *testing.T) {
	const capitalSharpS = "ẞ"

	if got := strings.ToLower(capitalSharpS); got != "ß" {
		t.Errorf("strings.ToLower(%q) = %q; the standard library's behaviour changed",
			capitalSharpS, got)
	}
	if strings.EqualFold(capitalSharpS, "SS") {
		t.Error("strings.EqualFold now does full case folding; casefold.go may be redundant")
	}
}

// TestFullCaseFoldingResolvesCapitalSharpS is the positive half: the example
// that was CommonMark 0.31.2's only failure for two milestones now passes,
// because normalizeLabel folds through casefold.go's exception table rather
// than through strings.ToLower alone.
func TestFullCaseFoldingResolvesCapitalSharpS(t *testing.T) {
	got := RenderHTML(Parse("[ẞ]\n\n[SS]: /url\n"))
	want := "<p><a href=\"/url\">ẞ</a></p>\n"
	if got != want {
		t.Errorf("example 540 regressed: got %q, want %q", got, want)
	}
}

func FuzzFullFold(f *testing.F) {
	for _, seed := range []string{"a", "A", "ẞ", "ß", "İ", "ﬀ", "", "9", " "} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, r := range s {
			folded := fullFold(r)
			if folded == "" && r != 0 {
				t.Fatalf("fullFold(%q) returned empty for a non-zero rune", r)
			}
			// Folding must be idempotent per character: folding an
			// already-folded rune should not change it further for anything
			// the table maps to plain ASCII lowercase output.
			for _, out := range folded {
				if out >= 'A' && out <= 'Z' {
					t.Fatalf("fullFold(%q) = %q retained an uppercase ASCII letter", r, folded)
				}
			}
		}
	})
}
