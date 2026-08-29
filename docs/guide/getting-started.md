# Getting started

Point `bindery` at a directory of Markdown:

```bash
bindery dev ./docs
```

The dev server watches the directory and reloads the browser on save.

## Building

```bash
bindery build ./docs --out ./site
```

The output is plain HTML with no JavaScript at all: the live-reload client is
injected by `dev` and never by `build`.

## What it does not do

* No configuration file
* No plugins
* No JavaScript in published output
