package main

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"io"
	"sort"
	"strings"
)

// CommonMark conformance.
//
// The suite is the 652 examples published with the specification. Each is a
// pair of Markdown and the HTML the reference implementation produces, compared
// byte for byte -- no normalisation, no whitespace forgiveness. That strictness
// is the point: a conformance number computed against a lenient comparison
// would not mean anything.
//
// The fixtures are embedded so that "bindery spec" works from the binary alone,
// with nothing to fetch and no path to get wrong. They are the only data in this
// repository that was not written for it, and they are disclosed as such in
// STDLIB.md.

//go:embed testdata/spec.json
var specJSON []byte

// specExample is one entry from the published suite.
type specExample struct {
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	Example  int    `json:"example"`
	Section  string `json:"section"`
}

func loadSpec() ([]specExample, error) {
	var examples []specExample
	if err := json.Unmarshal(specJSON, &examples); err != nil {
		return nil, fmt.Errorf("reading the embedded spec fixtures: %w", err)
	}
	return examples, nil
}

// specResult is the outcome of running the suite.
type specResult struct {
	Total    int
	Passed   int
	Sections []sectionResult
	Failures []specFailure
}

type sectionResult struct {
	Name   string
	Total  int
	Passed int
}

type specFailure struct {
	Example  int
	Section  string
	Markdown string
	Want     string
	Got      string
}

// runSpec renders every example and compares the output.
func runSpec(examples []specExample, section string) specResult {
	var result specResult
	index := map[string]int{}

	for _, example := range examples {
		if section != "" && !strings.EqualFold(example.Section, section) {
			continue
		}
		i, seen := index[example.Section]
		if !seen {
			i = len(result.Sections)
			index[example.Section] = i
			result.Sections = append(result.Sections, sectionResult{Name: example.Section})
		}

		result.Total++
		result.Sections[i].Total++

		got := RenderHTML(Parse(example.Markdown))
		if got == example.HTML {
			result.Passed++
			result.Sections[i].Passed++
			continue
		}
		result.Failures = append(result.Failures, specFailure{
			Example:  example.Example,
			Section:  example.Section,
			Markdown: example.Markdown,
			Want:     example.HTML,
			Got:      got,
		})
	}
	return result
}

// Rate returns the pass rate as a percentage.
func (r specResult) Rate() float64 {
	if r.Total == 0 {
		return 0
	}
	return 100 * float64(r.Passed) / float64(r.Total)
}

// report writes a human-readable summary, worst sections first, so that the
// output says where the remaining work is rather than only how much there is.
func (r specResult) report(w io.Writer, colour bool, verbose bool) {
	sections := append([]sectionResult(nil), r.Sections...)
	sort.Slice(sections, func(i, j int) bool {
		ri, rj := sections[i].rate(), sections[j].rate()
		if ri != rj {
			return ri < rj
		}
		return sections[i].Name < sections[j].Name
	})

	fmt.Fprintf(w, "CommonMark %s, %d examples\n\n", specVersion, r.Total)
	for _, s := range sections {
		mark := "  "
		if s.Passed == s.Total {
			mark = "ok"
		}
		fmt.Fprintf(w, "  %-2s %-34s %3d/%-3d  %5.1f%%\n",
			mark, s.Name, s.Passed, s.Total, s.rate())
	}

	if verbose {
		for _, f := range r.Failures {
			fmt.Fprintf(w, "\n--- example %d (%s)\n", f.Example, f.Section)
			fmt.Fprintf(w, "markdown: %q\n", f.Markdown)
			fmt.Fprintf(w, "want:     %q\n", f.Want)
			fmt.Fprintf(w, "got:      %q\n", f.Got)
		}
	}

	fmt.Fprintf(w, "\n%d/%d (%.1f%%)\n", r.Passed, r.Total, r.Rate())
	if len(r.Failures) > 0 && !verbose {
		fmt.Fprintf(w, "run with --verbose to see the %d failing example%s\n",
			len(r.Failures), plural(len(r.Failures)))
	}
}

func (s sectionResult) rate() float64 {
	if s.Total == 0 {
		return 0
	}
	return 100 * float64(s.Passed) / float64(s.Total)
}

// specVersion is the version of the specification the embedded fixtures come
// from. It is stated in output so that a score is never quoted without saying
// what it was measured against.
const specVersion = "0.31.2"
