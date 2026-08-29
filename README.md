# bindery

[![verify](https://github.com/Yash-vs9/bindery/actions/workflows/verify.yml/badge.svg)](https://github.com/Yash-vs9/bindery/actions/workflows/verify.yml)

A folder of Markdown becomes a searchable documentation site with live reload.
One binary. Zero dependencies.

The badge above is not decoration: it is the dependency proof running on a
machine neither of us controls, on every push, on Linux, macOS and Windows --
confirming the manifest is empty, every transitive import is standard library,
the binary is byte-reproducible, and the full test and fuzz suites pass. Click
it for the log rather than taking `deps-proof.txt` on trust.

```
bindery dev ./docs      # localhost:8080, live reload on save
bindery build ./docs    # -> ./site, static HTML, deploy anywhere
bindery spec            # CommonMark conformance, reported honestly
bindery render FILE.md  # one file to stdout (--format html|ansi|json)
bindery pdf ./docs      # -> docs.pdf, one document, clickable links
```

## Conformance

**652 of 652 CommonMark examples pass — 100.0%**, against specification 0.31.2,
compared byte for byte against the reference output with no normalisation.

```bash
bindery spec              # the score, by section
bindery spec --verbose    # a diff for every failing example
```

This was 651/652 for two milestones. The one gap was example 540: `[ẞ]`
resolving against a `[SS]:` reference definition, which requires *full* Unicode
case folding — capital sharp s (U+1E9E) folds to the two characters `"ss"`, and
Go's standard library only has *simple* case folding, which maps one rune to
one rune and cannot express that. `golang.org/x/text/cases` has the full tables
and is exactly the dependency this project does not take.

The fix, in `src/casefold.go`, is a table of 266 exceptions generated from the
Unicode Consortium's own `CaseFolding.txt` — the code points where full folding
disagrees with what `strings.ToLower` already gets right — layered on top of
the standard library rather than replacing it. See `STDLIB.md` for how it was
generated and verified.

Doing this today usually means Node, npm, and several hundred transitive
packages to turn text files into other text files. bindery is one static binary
with an empty dependency manifest.

**Zero Dependency Hackathon 2026 — Track F (Open / Wildcard).**

## Why this is a wildcard entry

A documentation site generator is the kind of thing nobody attempts without
packages. It needs a Markdown parser, YAML front-matter, syntax highlighting, a
file watcher, a WebSocket for live reload, an HTTP server, and a search index.
Go's standard library provides exactly one of those seven: the HTTP server.

The other six are the project. `STDLIB.md` records each one, what it replaces,
and what the substitution cost.

## Build and run

Requires **Go 1.27.0**. No other tooling.

```bash
make build          # -> bin/bindery
make test           # standard library test suite
make verify         # everything a reviewer needs, in one command
```

## Repository layout

```
bindery/
├── README.md            what it does, how to run it, honest limits
├── STDLIB.md            every "I would normally import X, instead I used Y"
├── Makefile             one command to a runnable artifact
├── .github/workflows/   CI: runs the proof below on every push
├── src/                 the source, all written this weekend
│   ├── *.go             the program
│   ├── *_test.go        unit tests, beside the code they test
│   └── testdata/        the CommonMark conformance fixtures
├── tests/               end-to-end tests: build the binary, drive it, assert
├── go.mod               the manifest, with no require block
├── deps-proof.txt       output showing zero third-party dependencies
└── .zero-dep.toml       track letter and one-line pitch
```

**Why the tests are in two places.** Go requires `_test.go` files to sit in the
same package as the code they test, and the unit tests exercise unexported
identifiers — `parseBlocks`, `acceptKey`, `tokenise`, `slugify`. A separate
package cannot reach those without exporting the entire internal surface to
satisfy a directory name, which would be a worse repository, not a better one.
So the unit tests live in `src/` where the language puts them.

`tests/` holds the layer they cannot cover: that the documented build command
produces a working artifact, that exit codes are what this README promises, that
the dev server actually serves and actually reloads a file edited on disk, and
that two builds of the same input produce identical bytes. Those tests know
nothing about the internals. If every one of them passes, the tool works.

Within `src/` the package is flat rather than split into `internal/`
subpackages: for a single binary of this size a deep package tree is the
unidiomatic choice, so files are named for what they hold instead.

```
main.go              CLI: subcommands, flags, exit codes
markdown.go          the document model shared by both parser passes
block.go  scan.go    phase 1: block structure, line scanners
inline.go  emphasis.go  link.go  linkref.go  html_block.go
                     phase 2: inlines, the delimiter stack, links, raw HTML
render_html.go  render_ansi.go   two back ends over one model
highlight.go         table-driven syntax highlighting
frontmatter.go       the YAML subset
toc.go               slugs and the table of contents
search.go            the inverted index and BM25
site.go  theme.go    discovery, building, the page shell
serve.go  livereload.go  watch.go   the dev server, WebSocket, watcher
spec.go              the CommonMark conformance runner
term.go              ANSI styling, NO_COLOR, TTY detection
```

## Dependency proof

```bash
make verify         # also writes deps-proof.txt
```

`go.mod` has no `require` block, `go list -m all` reports only this module, and
`go list -deps .` reports standard-library packages only. The same checks run
in CI on every push, on Linux, macOS and Windows -- see the badge above, or
`.github/workflows/verify.yml`.

## Reproducible build

`make repro` builds the binary twice into separate trees and compares the bytes.

```
(hashes are regenerated at submission; the mechanism is proven in deps-proof.txt)
```

## Status

Under construction during the event window. Milestones, in order:

- [x] **M0** repo, CLI surface, exit-code contract, reproducible build proven
- [x] **M1** Markdown → HTML, theme, `build`
- [x] **M2** dev server, file watcher, live reload
- [x] **M3** CommonMark hardening, conformance score
- [x] **M4** syntax highlighting, front-matter, navigation
- [x] **M5** search index
- [ ] **M6** fuzzing, benchmarks, documentation

## Numbers

Measured on an Apple M-series laptop, Go 1.27.0, `go test -bench -count=5`, all
five runs within 1% of each other. Reproduce with `make bench`.

Two corpora. The **spec corpus** is every Markdown sample in the CommonMark
suite concatenated — about 100KB of deliberately awkward input, which is harder
than real documentation and is meant to be. The **prose corpus** is this
project's own README, STDLIB.md and docs: 35KB of the input a documentation tool
actually sees.

| | bindery | goldmark | |
|---|---|---|---|
| spec corpus, parse + render | **550 µs** · 29.4 MB/s | 855 µs · 19.0 MB/s | bindery 1.55× faster |
| prose corpus, parse + render | **410 µs** · 84.5 MB/s | 462 µs · 75.1 MB/s | bindery 1.13× faster |
| spec corpus, memory | 833 KB · 8,340 allocs | 858 KB · 7,374 allocs | comparable |
| prose corpus, memory | 818 KB · 4,146 allocs | **535 KB** · 2,758 allocs | **goldmark 1.5× leaner** |

So: bindery is faster on both corpora, and allocates substantially more on
realistic prose. That is the honest shape of it, and the memory result is the
one worth taking seriously — the parser builds a full block tree with per-node
slices and does not pool anything.

**Read the speed result with these caveats, all of which matter.**

- Both bindery and goldmark pass 652/652 CommonMark examples.
- goldmark supports extensions bindery does not implement at all: tables,
  strikethrough, task lists, footnotes, definition lists, typographer.
- goldmark is a configurable library with a plugin architecture and an AST
  designed for third-party extension. bindery is a tool with a parser inside it.
- Being faster is partly a consequence of doing less. This is not evidence that
  bindery is a better parser, only that it is a smaller one.
- One machine, one Go version, two corpora. Treat it as an order of magnitude,
  not a ranking.

The comparison harness was built in a throwaway module **outside this
repository** and is not shipped; measuring against the package you replaced
requires installing it, which is why it lives elsewhere and why `go.mod` here
stays empty. Rebuilding it takes about a minute and the method is described in
STDLIB.md.

### Other measurements

| | |
|---|---|
| Full site build, 3 pages | 3–4 ms |
| Reload after a save (parse, render, notify) | ~115 µs plus the watcher's poll interval |
| Search index for this site | 79 µs, 7.8 KB of JSON |
| Syntax highlighting | 45 MB/s |
| Whole 652-example conformance suite | 722 µs |

### Fuzzing

Ten fuzz targets, **17.9 million executions**, no crashes and no hangs.

Being straight about what that means: this sweep found nothing. The real bugs
this weekend were found by table-driven tests and by deliberately injecting
faults into `make verify` to see whether it noticed. Fuzzing is evidence the
parsers do not fall over on hostile input, which is worth having, and it is not
evidence that they are correct.

Three of the targets assert properties rather than mere survival: that no input
can escape an HTML attribute the renderer emits, that parsing and rendering are
deterministic, and that markup inside a code fence is never interpreted.

## Honest limits

Written as they become true, not at the end. So far:

- **Full Unicode case folding is a 266-entry exception table, not the real Unicode
  algorithm.** Go's standard library has only simple case mapping, which cannot
  express a fold from one rune into several — `casefold.go` covers exactly the
  code points where that matters (generated from the Unicode Consortium's
  `CaseFolding.txt`) and falls back to `strings.ToLower` everywhere else. It
  closes every CommonMark example this project's suite exercises, but it is not
  a general Unicode case-folding implementation and should not be mistaken for
  one — `golang.org/x/text/cases` is the real thing, and is exactly the
  dependency this project does not take.
- **Speed is measured, not assumed, and it is not always in bindery's favour.**
  See "Numbers" above: faster than goldmark on both benchmark corpora, and
  meaningfully hungrier for memory on realistic prose (1.5× the allocations).
  Read both numbers with the caveats listed there before treating either as a
  verdict on which parser is "better" — they measure different things doing
  different amounts of work.
- No tables, task lists, strikethrough, footnotes or autolink extensions.
  bindery implements CommonMark, not GitHub Flavored Markdown.
- **Front matter is a documented YAML subset**, not YAML: block mappings,
  sequences, scalars and comments. Anchors, aliases, tags, flow style, block
  scalars and multi-document streams are rejected with a line and column rather
  than misread. An unquoted `no` is the string `"no"`, not `false`.
- **Syntax highlighting is lexical, not grammatical.** Go, JavaScript,
  TypeScript, Python, shell, JSON and diff are described; anything else gets a
  generic lexer that handles quotes, digits and comments.
- **Terminal width is read from `COLUMNS`**, falling back to 80, because Go
  cannot ask a terminal how wide it is without a dependency. Resize a window
  without your shell updating `COLUMNS` and wrapping will be wrong.
- **Search has no stemming**: "parsing" does not match "parsed" unless one is a
  prefix of the other. Prefix matching scans the term list, which is fine for a
  documentation corpus and would not be for a hundred thousand terms.
- **The whole search index is downloaded on first search** — 7.8KB for this
  project's own docs, growing linearly with the corpus.
- **Tokens shorter than two characters are dropped**, so `C++` and `C#` are not
  searchable.
- The search index is fetched from an absolute `/search-index.json`, so a site
  served from a subdirectory will not find it.
- **Diagrams are a flowchart subset.** ```` ```mermaid ```` fences render as
  inline SVG: `graph TD` and `graph LR`, four node shapes, four arrow kinds,
  edge labels. No subgraphs, no styling directives, no sequence, class or Gantt
  diagrams. A fence that does not parse falls back to a code block.
- **Diagram layout is one ordering pass, not an iterated one**, and edges are
  straight lines rather than splines. Edges spanning more than one layer are
  routed around the side of the graph rather than through the nodes between
  them. Self-loops are parsed and not drawn.
- **In PDF output a diagram becomes its written description**, the same text a
  screen reader is given for the SVG. Drawing it would mean a second layout
  target; printing the source would be worse than either.
- **PDF output is Latin text only.** It uses the base-14 fonts every reader
  already has, which is what removes font parsing and rasterisation from the
  problem — and what limits it. Smart quotes and dashes are transliterated;
  CJK, Cyrillic and emoji become question marks. Showing them would mean
  embedding a font, which means parsing one.
- **PDF layout has no widow or orphan control** beyond keeping a heading with
  the two lines that follow it, and no hyphenation.
- **Display width is approximated.** Wide characters and combining marks are
  handled by range checks rather than the Unicode width tables, so some emoji
  sequences and rare scripts will misalign.
- `render --format=ansi` awaits the terminal renderer (M4); it exits 1 and says
  so rather than pretending to work.
- The file watcher **polls** every 250ms, because Go's standard library has no
  watch API. Changes are debounced by 120ms, so a save is reflected in the
  browser within roughly 400ms worst case.
- The dev server speaks HTTP/1.1 with no TLS and no compression. It is a
  development server and nothing about it is meant for production.
- A tab is consumed whole when matching container indentation. Where the
  specification would have a tab straddle the boundary, contributing two of its
  four columns, bindery stops short. This will cost a handful of spec examples.

## Licence

MIT. See `LICENSE`.
