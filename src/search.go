package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Full-text search.
//
// This replaces lunr.js, fuse.js and flexsearch: an inverted index built at
// build time, emitted as JSON, and ranked in the browser by about a hundred
// lines of hand-written JavaScript.
//
// THE TOKENISER RULE, which is implemented twice and must not drift:
//
//	lowercase the text, split on every rune that is not a Unicode letter or
//	digit, discard tokens shorter than two characters, discard stop words.
//
// Once in Go (tokenise, below) and once in JavaScript (searchScript in
// theme.go, which uses /[^\p{L}\p{N}]+/u for the same split). If the two ever
// disagree, queries silently return nothing -- the worst kind of bug, because
// it looks like an empty index rather than a broken one. TestTokeniseFixtures
// pins the Go side against a table, and the same table is reproduced in the
// JavaScript comment so a future edit has something to check against.
//
// Indexing is per section rather than per page: a heading and the prose under
// it. That makes results deep-link to an anchor instead of dumping the reader
// at the top of a long page, and it makes ranking sharper, since a match in a
// short section counts for more than the same match in a long one.

// stopWords are dropped from both the index and queries. The list is short and
// hand-picked: aggressive stop-word removal hurts a documentation search, where
// "how to use it" is a plausible query and "no" is a plausible answer.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "for": true, "from": true, "has": true,
	"in": true, "into": true, "is": true, "it": true, "its": true, "of": true,
	"on": true, "or": true, "that": true, "the": true, "their": true,
	"there": true, "these": true, "they": true, "this": true, "to": true,
	"was": true, "were": true, "will": true, "with": true,
}

// searchDoc is one indexed section.
//
// The JSON field names are short because they repeat once per section and the
// index is downloaded by every visitor. It is the one place in this project
// where brevity beats clarity, and it is confined to the wire format.
type searchDoc struct {
	URL     string `json:"u"`
	Title   string `json:"t"`           // the page title
	Heading string `json:"h,omitempty"` // the section heading, if not the page root
	Length  int    `json:"n"`           // token count, for length normalisation
	Preview string `json:"p"`           // first line of prose, shown in results
}

// posting is one term occurrence: which document, and how often.
type posting struct {
	Doc  int
	Freq int
}

// MarshalJSON writes a posting as a two-element array. A struct with named
// fields would triple the size of the index for no benefit to a reader who is
// never expected to read it.
func (p posting) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]int{p.Doc, p.Freq})
}

// SearchIndex is the whole searchable corpus.
type SearchIndex struct {
	Docs      []searchDoc          `json:"docs"`
	Terms     map[string][]posting `json:"terms"`
	AvgLength float64              `json:"avg"`
}

// tokenise applies the shared tokeniser rule.
func tokenise(text string) []string {
	var tokens []string
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		token := current.String()
		current.Reset()
		if len(token) < 2 || stopWords[token] {
			return
		}
		tokens = append(tokens, token)
	}

	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// BuildSearchIndex indexes every page in the site.
func BuildSearchIndex(s *Site) *SearchIndex {
	index := &SearchIndex{Terms: map[string][]posting{}}
	total := 0

	for _, page := range s.Pages {
		for _, section := range sections(page) {
			tokens := tokenise(section.Title + " " + section.Body)
			if len(tokens) == 0 {
				continue
			}
			docID := len(index.Docs)
			index.Docs = append(index.Docs, searchDoc{
				URL:     section.URL,
				Title:   page.Title,
				Heading: section.Heading,
				Length:  len(tokens),
				Preview: preview(section.Body),
			})
			total += len(tokens)

			// Term frequencies for this section, then one posting per term.
			freq := map[string]int{}
			for _, token := range tokens {
				freq[token]++
			}
			for term, n := range freq {
				index.Terms[term] = append(index.Terms[term], posting{Doc: docID, Freq: n})
			}
		}
	}

	if len(index.Docs) > 0 {
		index.AvgLength = float64(total) / float64(len(index.Docs))
	}

	// Postings are built by iterating a map, so they arrive in random order.
	// Sorting is what makes the emitted index byte-identical between builds,
	// which the reproducible-build claim depends on.
	for term := range index.Terms {
		postings := index.Terms[term]
		sort.Slice(postings, func(i, j int) bool { return postings[i].Doc < postings[j].Doc })
	}
	return index
}

// section is a heading and the prose beneath it.
type section struct {
	Heading string
	Title   string
	URL     string
	Body    string
}

// sections splits a page at its level-two and level-three headings.
func sections(page *Page) []section {
	var out []section
	current := section{Title: page.Title, URL: page.URL}
	var body strings.Builder

	flush := func() {
		current.Body = body.String()
		out = append(out, current)
		body.Reset()
	}

	for _, block := range page.Doc.Root.Children {
		if block.Kind == KindHeading && block.Level >= 2 && block.Level <= 3 {
			flush()
			heading := strings.TrimSpace(plainText(block.Inlines))
			current = section{
				Heading: heading,
				Title:   page.Title,
				URL:     page.URL + "#" + block.slug,
			}
			continue
		}
		body.WriteString(blockText(block))
		body.WriteString(" ")
	}
	flush()
	return out
}

// blockText flattens a block to searchable text.
//
// Code blocks are included deliberately: in documentation, the identifier
// someone is searching for is often only in an example.
func blockText(b *Block) string {
	switch b.Kind {
	case KindCodeFenced, KindCodeIndented, KindHTMLBlock:
		return b.Text()
	}
	var sb strings.Builder
	sb.WriteString(plainText(b.Inlines))
	for _, child := range b.Children {
		sb.WriteString(" ")
		sb.WriteString(blockText(child))
	}
	return sb.String()
}

// preview returns a short snippet for the results list.
func preview(body string) string {
	const limit = 120
	text := strings.Join(strings.Fields(body), " ")
	if len(text) <= limit {
		return text
	}
	// Cut at a word boundary rather than mid-word, and never mid-rune.
	cut := strings.LastIndexByte(text[:limit], ' ')
	if cut < limit/2 {
		cut = limit
		for cut > 0 && !isRuneStart(text[cut]) {
			cut--
		}
	}
	return strings.TrimSpace(text[:cut]) + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// JSON renders the index for the browser.
//
// json.Deterministic(true) is load-bearing, not decoration. encoding/json v1
// sorted map keys on the way out; encoding/json/v2 does not, and emits them in
// Go's deliberately randomised map iteration order. The index is a map of terms
// to postings, so without this option two builds of identical source produce
// different bytes and the reproducible-build claim quietly stops being true.
//
// This was caught by TestSearchIndexIsDeterministic, which marshals the same
// index nine times and compares. It failed on the first run.
func (idx *SearchIndex) JSON() ([]byte, error) {
	return json.Marshal(idx, jsontext.WithIndent(""), json.Deterministic(true))
}

// IDF is the inverse document frequency of a term, exposed for tests. The
// browser computes the same value; this is here so the ranking can be checked
// in Go rather than only by eye.
func (idx *SearchIndex) IDF(term string) float64 {
	n := float64(len(idx.Terms[term]))
	total := float64(len(idx.Docs))
	if n == 0 {
		return 0
	}
	return math.Log(1 + (total-n+0.5)/(n+0.5))
}

// Score ranks a document for a query using BM25, mirroring the browser.
func (idx *SearchIndex) Score(query string, doc int) float64 {
	const k1, b = 1.2, 0.75
	score := 0.0
	for _, term := range tokenise(query) {
		for _, p := range idx.Terms[term] {
			if p.Doc != doc {
				continue
			}
			tf := float64(p.Freq)
			norm := 1 - b + b*float64(idx.Docs[doc].Length)/idx.AvgLength
			score += idx.IDF(term) * tf * (k1 + 1) / (tf + k1*norm)
		}
	}
	return score
}
