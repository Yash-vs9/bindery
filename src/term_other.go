//go:build !windows

package main

import "os"

// enablePlatformANSI is a no-op everywhere ANSI escapes already render without
// asking permission first. See term_windows.go for the one platform that is
// not true on.
func enablePlatformANSI(f *os.File) {}
