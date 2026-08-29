---
title: Edge cases
order: 3
---
# Edge cases

The parser is written against the CommonMark specification, which means the
awkward cases matter more than the ordinary ones.

## Emphasis

Nested emphasis resolves with a delimiter stack: *outer **inner** outer*.
Intraword underscores are left alone, so snake_case_names survive, while
intraword asterisks do not: foo*bar*baz.

## Code spans

A code span can contain a backtick if the fence is longer: `` ` ``. Markup
inside a code span stays literal: `*not emphasis*`.

## Escapes and entities

Backslashes escape punctuation: \*not emphasis\*. Named entities resolve
through the standard library's HTML tables: &copy; &mdash; &hellip;.

## Blockquotes

> Lazy continuation means this second line
belongs to the same paragraph inside the quote.

> > Quotes nest.

## Fenced code keeps markup literal

```
# not a heading
> not a quote
*not emphasis*
```

## Links

An [inline link](https://example.com "with a title"), an autolink
<https://example.com>, and an email <hello@example.com>.

Links do not nest: [outer [inner](https://inner.example) outer](https://outer.example).

## Tables

GFM tables are not part of CommonMark, so this is an opt-in extension —
`bindery spec` never enables it, and the 652/652 conformance score is
unaffected either way.

| Feature      | Standard | Notes                                    |
|:-------------|:--------:|:------------------------------------------|
| Tables       |   GFM    | off in `bindery spec`, on everywhere else |
| Diagrams     |   n/a    | `mermaid` fences, rendered as inline SVG  |
| Case folding | Unicode  | 266-entry exception table, not a full impl |

A cell may contain inline code with a pipe, like `` `a|b` ``, without splitting.
