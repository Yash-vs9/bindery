package main

import (
	"os"
	"testing"
)

// TestEnablePlatformANSIDoesNotPanic exercises whichever implementation the
// build is compiling for -- the real syscall on Windows, the no-op elsewhere.
// The Windows path itself can only be exercised on Windows, which is why CI
// runs the full suite on windows-latest rather than only vetting it there.
func TestEnablePlatformANSIDoesNotPanic(t *testing.T) {
	enablePlatformANSI(os.Stdout)
	enablePlatformANSI(os.Stderr)

	// A second call must also be safe -- on Windows this exercises the
	// sync.Once guard rather than repeating the syscall.
	enablePlatformANSI(os.Stdout)
}
