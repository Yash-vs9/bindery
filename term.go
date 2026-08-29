package main

import (
	"io"
	"os"
	"strings"
)

// Terminal styling.
//
// This is the whole of what chalk (319.8M downloads a week) is imported for: a
// handful of escape sequences and the discipline to omit them when nobody can
// see them. Go has no styling in its standard library and needs none.

// ANSI select-graphic-rendition sequences.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
)

// colorEnabled reports whether w should receive escape sequences.
//
// Two conditions, both necessary. NO_COLOR is honoured because it is the
// convention (https://no-color.org) and because a build log full of escape
// sequences is worse than no colour at all. The TTY check uses os.FileMode's
// ModeCharDevice bit, which is how you ask this question with only the standard
// library: a pipe or a file is not a character device.
func colorEnabled(w io.Writer) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// style wraps s in the given sequences when w is a terminal.
func style(w io.Writer, s string, codes ...string) string {
	if !colorEnabled(w) {
		return s
	}
	return strings.Join(codes, "") + s + ansiReset
}

func tick(w io.Writer) string  { return style(w, "ok", ansiGreen, ansiBold) }
func cross(w io.Writer) string { return style(w, "fail", ansiRed, ansiBold) }
func dim(w io.Writer, s string) string {
	return style(w, s, ansiDim)
}
