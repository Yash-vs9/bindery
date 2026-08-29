# bindery

A folder of Markdown becomes a searchable documentation site with live reload.
One binary. Zero dependencies.

```
bindery dev ./docs      # localhost:8080, live reload on save
bindery build ./docs    # -> ./site, static HTML, deploy anywhere
bindery spec            # CommonMark conformance, reported honestly
bindery render FILE.md  # one file to stdout (--format html|ansi|json)
```

## Conformance

**651 of 652 CommonMark examples pass — 99.8%**, against specification 0.31.2,
compared byte for byte against the reference output with no normalisation.

```bash
bindery spec              # the score, by section
bindery spec --verbose    # a diff for every failing example
```

Every section is at 100% except Links, at 89/90. The single failure is link
labels needing *full* Unicode case folding, which Go's standard library does not
have — see "Honest limits" below and the entry in `STDLIB.md`. It is not
special-cased, on purpose.

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

## Dependency proof

```bash
make verify         # also writes deps-proof.txt
```

`go.mod` has no `require` block, `go list -m all` reports only this module, and
`go list -deps .` reports standard-library packages only.

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
- [ ] **M5** search index
- [ ] **M6** fuzzing, benchmarks, documentation

## Honest limits

Written as they become true, not at the end. So far:

- **One CommonMark example fails**, and it is example 540: a link label written
  `[ẞ]` should match a definition written `[SS]`, because the specification uses
  full Unicode case folding. Go's standard library has only simple case mapping —
  `strings.ToLower("ẞ")` is `"ß"`, and `strings.EqualFold` does simple folding —
  and neither can turn one rune into two. The fix would be a third-party
  dependency, so bindery fails the example rather than special-casing the
  character to make a number look better.
- **It is slower than the parser it replaces.** Roughly 18 MB/s over the whole
  spec corpus and 27 MB/s over ordinary documentation Markdown, measured with
  `go test -bench`. goldmark is several times faster. For a documentation site
  of a few hundred files this is microseconds per page and does not matter, but
  it is not a fast parser and is not claimed to be.
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
