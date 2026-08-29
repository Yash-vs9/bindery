# STDLIB.md

Every capability in bindery that would normally arrive as a package, and what I
used instead. This file is written as each substitution lands, not reconstructed
afterwards, so the order below is roughly the order the code was written.

Download figures are weekly npm downloads, checked against npmjs.com on the date
in the row. Where I have not checked a figure yet the row says so rather than
guessing.

## Code and data I did not write this weekend

Listed first, because it matters most.

- **The RFC 6455 WebSocket accept GUID** (`258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
  and the worked example that verifies it, both from RFC 6455 §1.3. A constant
  and a test vector defined by the specification, transcribed from the RFC text
  rather than copied from any implementation.

- **`testdata/spec.json`** — the official CommonMark conformance fixtures,
  version 0.31.2, from <https://spec.commonmark.org/0.31.2/spec.json>. Data, not
  code: 652 `{markdown, html}` pairs. The specification and its test data are
  published by John MacFarlane under CC-BY-SA 4.0. The file is embedded in the
  binary with `go:embed` so that `bindery spec` runs anywhere, and it is used
  only to measure conformance — no part of it is consulted while parsing.

## Substitutions

### CLI argument parsing
**Instead of** `cobra` / `commander` / `minimist` (80.5M weekly)
**I used** `flag`, with one `flag.NewFlagSet` per subcommand.

`flag` has no subcommand concept, so `run` dispatches on `args[0]` and hands the
remainder to a per-command flag set built with `flag.ContinueOnError`. That last
detail is the whole reason this works cleanly: the default `flag.ExitOnError`
calls `os.Exit` from inside the parser, which makes exit codes untestable. With
`ContinueOnError` every failure returns through one path, so `bindery` can
promise exit 2 for a bad command line and prove it in a test.

**Cost:** no automatic subcommand help tree, no shell completion. Roughly 40
lines of dispatch and usage text I would not have written otherwise.

### Test framework
**Instead of** `testify` / `jest` / `mocha`
**I used** `testing` with table-driven subtests via `t.Run`.

`run` takes its argument list and both output streams as parameters and returns
an exit code instead of calling `os.Exit`, which makes the entire CLI surface
testable in-process with `bytes.Buffer`. No assertion library is needed for
that; `if got != want { t.Errorf(...) }` is the idiom.

**Cost:** no assertion DSL, no mocking framework. Neither is missed.

### Reproducible builds
**Instead of** a build-stamping tool or a release pipeline
**I used** `go build -trimpath -buildvcs=false -ldflags="-s -w -buildid="` with
`CGO_ENABLED=0`, and a `version` constant in the source.

The usual instinct is to inject a version and a build timestamp with `-ldflags
-X`. A timestamp is exactly what makes a build non-reproducible, so the version
is a source constant instead. `-trimpath` removes absolute paths, `-buildvcs=false`
keeps git state out of the binary, and `-buildid=` clears the last varying field.
`make repro` builds twice into separate trees and fails if the bytes differ.

**Cost:** the version has to be edited in source at release time rather than
derived from a tag.

### Markdown parsing
**Instead of** `marked` / `markdown-it` / `goldmark` / `blackfriday`
**I used** a hand-written CommonMark parser: `block.go`, `scan.go`, `inline.go`,
`emphasis.go`, `link.go`, `render_html.go`.

No standard library in any language ships a Markdown parser, so this is the
project rather than a substitution of convenience. It follows the
specification's own two-pass structure: a line-oriented pass builds the block
tree and collects link reference definitions, then a character-oriented pass
resolves inlines. The passes cannot be fused, because a reference definition may
appear after the paragraph that uses it.

Emphasis is the part that resists shortcuts. Whether a `*` may open or close
depends on the characters either side of it, and in `*foo **bar** baz*` the
delimiters do not pair by position, so the specification prescribes a delimiter
stack. It is implemented as written, including the openersBottom bookkeeping
that keeps pathological input linear rather than quadratic, and the rule of
three that decides `*foo**bar*`.

**Result: 651 of 652 examples, 99.8%**, against CommonMark 0.31.2, compared
byte for byte with no normalisation. `bindery spec` prints the score and
`go test` fails if it drops. Every section is at 100% except Links, where one
example fails for a reason documented below.

**Cost:** roughly 2,000 lines and the majority of the build. It is also slower
than the parser it replaces: bindery renders mixed Markdown at about 18–27 MB/s,
where goldmark is several times faster. The number is published in README.md
rather than omitted.

### HTML entity resolution
**Instead of** `he` / `entities`
**I used** `html.UnescapeString`.

CommonMark requires resolving named character references, which means the whole
HTML5 entity table -- around 2,100 entries. `html.UnescapeString` carries it.
The validity test is simply whether unescaping changed the text, since the
function leaves unknown references alone.

**Cost:** none. This is the standard library doing real work for free.

Note the mirror-image decision in `render_html.go`: escaping is hand-written
rather than using `html.EscapeString`, because that function escapes `'` as
`&#39;` and the reference output does not, which would cost conformance.

### Page templating
**Instead of** `handlebars` / `ejs` / `nunjucks`
**I used** `html/template`, with the parse wrapped in `sync.OnceValue`.

`html/template` is escaping-first rather than a general templating language,
which is the right trade for a page shell: interpolation into an `href` is
escaped as a URL and interpolation into element content as HTML, contextually,
without the author remembering to ask. The rendered Markdown body is the single
value marked `template.HTML`, because the Markdown renderer escaped it already.

`sync.OnceValue` (Go 1.21) replaces the `once_cell`-style lazy static this would
otherwise want, and keeps template parsing off the path of commands that do not
render, such as `bindery version`.

**Cost:** no template inheritance, no partials, no loops beyond `range`. For one
page shell, none of that is missed.

### Flags after positional arguments
**Instead of** `cobra` / `commander`'s argument permutation
**I used** twenty lines of permutation in `main.go` (`parse`).

This one was found by a bug, not by planning. Package `flag` stops parsing at
the first non-flag argument, by design, so `bindery build docs --out site`
treated `--out` as a positional and silently ignored it -- the site went to the
default directory. Every CLI users expect to behave otherwise permutes flags
ahead of positionals first, so `parse` does that before handing the result to
`flag.Parse`.

Whether a flag consumes the next argument has to be asked of the flag set:
booleans do not, everything else does. `flag.Value` does not expose that, but
the `IsBoolFlag() bool` convention that package `flag` uses internally is
reachable through a type assertion.

**Cost:** twenty lines, and one silently-wrong build before it was noticed.

### Terminal colour
**Instead of** `chalk` (319.8M weekly downloads, verified in the event's own
cheat-sheet)
**I used** raw ANSI escape sequences in `term.go`.

Six constants and one wrapper. The part worth more than the escape sequences is
the discipline around them: `NO_COLOR` is honoured per the convention, `TERM=dumb`
is honoured, and output is only styled when the destination is a character
device -- checked with `os.FileMode`'s `ModeCharDevice` bit, which is how you ask
"is this a terminal?" with nothing but the standard library. A build log full of
escape sequences is worse than no colour at all.

Go also has `util.styleText`'s equivalent of nothing: unlike Node, the standard
library offers no styling helper, so this is a genuine gap rather than a
convenience.

**Cost:** no 256-colour or truecolour detection, no nested-style composition.

### JSON output
**Instead of** `json-iterator/go`
**I used** `encoding/json/v2` with `encoding/json/jsontext` for indentation.

Both graduated out of `GOEXPERIMENT` in Go 1.27, which was verified on this
machine before the row was written: `go doc encoding/json/v2` resolves with no
build tag. `jsontext.WithIndent` supplies formatting as an option rather than a
separate `MarshalIndent` entry point.

Block and inline kinds are integer constants, so they implement
`encoding.TextMarshaler` to appear in output as names. Both `encoding/json` and
`encoding/json/v2` consult that interface.

**Cost:** none, given Go 1.27. On any earlier toolchain this row would not exist.

### WebSocket
**Instead of** `gorilla/websocket` / `nhooyr.io/websocket` / `ws`
**I used** RFC 6455 implemented directly in `livereload.go`, on top of
`crypto/sha1`, `encoding/base64`, `encoding/binary` and `http.ResponseController`.

Go's standard library has no WebSocket. `net/http` carries the handshake's HTTP
half -- it is an ordinary GET with an `Upgrade` header -- and then stops. About
150 lines covers the opening handshake, unmasked server-to-client framing in all
three length encodings, and enough of the read path to answer pings and honour
close.

`http.NewResponseController(w).Hijack()` is the modern route to the underlying
connection and works through wrappers where a direct assertion to
`http.Hijacker` does not.

**Two things worth recording honestly.** First, Server-Sent Events would have
done this job in roughly twenty lines with `net/http` and a `Flush`, and for
one-way reload notifications SSE is arguably the better engineering. WebSocket
is here because `gorilla/websocket` is a dependency worth deleting outright and
because doing it correctly is not expensive. The choice was deliberate.

Second: the first version of the magic GUID in this file was wrong. It read
`...95CA-5AB0DC85B11A` instead of `...95CA-C5AB0DC85B11` -- one character
adrift, a perfectly valid-looking GUID, and no browser on earth would have
completed the handshake. It was caught because `TestAcceptKey` asserts the
worked example from RFC 6455 §1.3, including the intermediate SHA-1 digest the
RFC publishes. A constant that looks right is not right, and the only defence
is a vector from the specification.

**Cost:** 150 lines, and no support for fragmentation, extensions, compression
or subprotocols. None are needed to send the string "reload".

### File watching
**Instead of** `fsnotify` / `chokidar` / `nodemon`
**I used** polling with a debounce in `watch.go`.

This is the largest genuine hole in Go's standard library for this project.
There is no watch API at all -- no inotify, no FSEvents, no kqueue wrapper. So
bindery stats the tree every 250ms and rebuilds once the tree has been quiet for
120ms.

The debounce is not a nicety. A single editor save produces several filesystem
events -- write, truncate, rename, chmod -- and "Save All" produces a burst
across many files. Without debouncing, one save means four rebuilds and four
browser reloads.

Polling has one real advantage worth naming: it cannot miss a change and cannot
deliver a spurious one, which the platform notification APIs both manage to do
under the write-to-temp-then-rename pattern most editors use.

**Cost:** change latency bounded by the poll interval rather than immediate, and
a small constant of stat traffic. Both measured and stated in README.md.

### Fake timers in tests
**Instead of** `jest.useFakeTimers` / `sinon` / a hand-rolled injectable clock
**I used** `testing/synctest` (stable in Go 1.25; use `synctest.Test`, as
`synctest.Run` is deprecated).

Inside a synctest bubble the clock is virtual: `time.Sleep`, `time.Ticker` and
`time.Timer` advance instantly once every goroutine in the bubble is blocked.
The debounce tests simulate 500ms of watcher activity and complete in 0.00s,
deterministically, with no injected clock interface threaded through the
production code.

This is the substitution most likely to be unfamiliar, and it is the one that
turned the flakiest tests in the project into the most reliable ones. Testing a
debounce with real sleeps is how you get a suite that fails once a fortnight on
a loaded CI machine.

**Cost:** none. It is strictly better than the alternatives.

### HTTP routing
**Instead of** `gorilla/mux` / `chi` / `express`
**I used** `net/http.ServeMux` with the method and wildcard patterns added in
Go 1.22.

`GET /__bindery/live` and `GET /` in the same mux, with the catch-all running
only when nothing more specific matches. No middleware chain, no router
package, no special-casing inside a single handler.

**Cost:** none at this scale.

### Path traversal protection
**Instead of** a static-file middleware
**I used** `os.Root` (Go 1.24) via `os.OpenRoot`.

`os.Root` resolves paths inside a directory and refuses to escape it, whatever
the encoding of the request. The hand-rolled alternative is `filepath.Clean`
plus a prefix check, which is the classic way to get this wrong -- and
`TestServeRefusesTraversal` writes the awkward paths onto a raw socket, because
Go's own HTTP client normalises them away before they would ever reach the
server.

**Cost:** none. This is newer and better than what most middleware does.

### Full Unicode case folding — the one thing the standard library cannot do
**I would have used** `golang.org/x/text/cases`
**and there is no standard-library answer.**

CommonMark normalises link labels with *full* Unicode case folding, under which
U+1E9E, capital sharp s, folds to the two characters `ss` — so `[ẞ]` matches a
definition written `[SS]`. Go's standard library has only *simple* case mapping:

    strings.ToLower("ẞ")           == "ß"     // one rune to one rune
    strings.EqualFold("ẞ", "SS")   == false   // simple folding, not full

Neither can map one rune to two, and no other package in the standard library
carries the full folding table. `golang.org/x/text` has it, and is exactly the
kind of dependency this project does not take — the event's rules are explicit
that `golang.org/x` is not a free pass.

So this is the single conformance example bindery fails, and it fails on purpose.
Special-casing sharp s would pass the test without implementing the rule, which
is tuning to the suite rather than to the specification, and it would make the
99.8% mean less. `TestUnicodeCaseFoldingIsSimpleNotFull` asserts the gap and will
start failing if the standard library ever closes it.

**Cost:** one conformance example, and an honest note instead of a workaround.

### Syntax highlighting
**Instead of** `highlight.js` / `prism` / `chroma`
**I used** one table-driven lexer in `highlight.go`.

Rather than a state machine per language, there is a single scanner driven by a
`langSpec`: comment markers, string delimiters with their escaping and
multiline rules, and keyword sets. Adding a language means writing down its
tables. Go, JavaScript/TypeScript, Python, shell, JSON and diff are described,
and an unknown language falls back to a generic description that gets quotes,
digits and comments right — usually better than no colour at all.

The invariant that matters is tested directly: stripping the emitted tags must
return the input unchanged. `TestHighlightPreservesText` and a fuzz target
assert it across every language, so highlighting can never silently corrupt the
code it colours.

**Cost:** lexical only, with no parser and no symbol table, so a word that is a
keyword anywhere is a keyword everywhere. That is the same trade every
regex-based highlighter makes, and it is invisible at the size of a
documentation code sample.

### YAML front matter
**Instead of** `js-yaml` / `gopkg.in/yaml.v3`
**I used** a documented subset in `frontmatter.go`.

Go's standard library has no YAML, and full YAML is a bigger project than the
Markdown parser: anchors, aliases, tags, merge keys, flow style and five kinds
of block scalar. What front matter actually needs is block mappings, block
sequences, scalars and comments, so that is what is implemented — and anything
outside it is a clear error with a line and column rather than a silent
misreading.

One deliberate deviation from YAML 1.1: an unquoted `no` is the string `"no"`,
not the boolean `false`. The Norway problem is the most common way configuration
files quietly corrupt data, and only `true` and `false` are booleans here.

The awkward case was telling a forgotten terminator from a document that simply
opens with a thematic break — both begin `---`. They are separated by whether
the next content line reads as a mapping entry, which turns a confusing silent
failure into a positioned error.

**Cost:** no anchors, aliases, tags, flow style, block scalars or multi-document
streams. All are rejected with an explanation rather than misread.

### Heading anchors
**Instead of** `github-slugger`
**I used** `slugify` in `toc.go`.

Letters and digits survive in any script, since `unicode.IsLetter` accepts
Cyrillic and CJK as readily as ASCII; everything else collapses to a single
separator. Collisions get a numeric suffix. The slug is stored on the heading
block when the table of contents is built, so the renderer and the sidebar
cannot disagree about what an anchor is called — deriving it twice would be one
derivation too many.

### Terminal Markdown rendering
**Instead of** `glow` / `glamour` / `mdcat`
**I used** `render_ansi.go`, a third back end over the same document model.

One parser, three renderers: HTML, terminal and JSON. `bindery render --format
ansi` reads a Markdown file in the terminal with wrapping, indented code,
quote bars and styled inlines.

### Terminal width — a gap with no standard-library answer
**I would have used** `golang.org/x/term`
**and there is no standard-library answer.**

Go cannot ask a terminal how wide it is. The `TIOCGWINSZ` ioctl is not exposed
by any standard-library package, and `golang.org/x/term` is a dependency the
rules do not exempt. So `terminalWidth` reads the `COLUMNS` environment
variable, which shells export, and falls back to eighty columns.

**Cost:** wrapping is wrong when a window is resized without the shell updating
`COLUMNS`. That is the honest limit of what the standard library allows.

### Display width — an approximation, declared as one
**I would have used** `golang.org/x/text/width` (or `go-runewidth`)
**and there is no standard-library answer.**

A rune is not a column. CJK ideographs and most emoji occupy two columns and
combining marks occupy none, so wrapping by `utf8.RuneCountInString` misaligns
anything outside Latin text. `displayWidth` skips ANSI escape sequences, treats
`unicode.Mn` and `unicode.Me` as zero width, and approximates the East Asian
Wide and Fullwidth ranges as two.

It is an approximation of the Unicode width tables, not those tables. It will be
wrong for some emoji sequences and rare scripts. Being approximately right
without a dependency beats being exactly right with one, but the approximation
is declared rather than hidden.
