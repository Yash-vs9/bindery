//go:build windows

package main

import (
	"os"
	"sync"
	"syscall"
)

// Windows console colour support.
//
// A plain cmd.exe, and older console hosts generally, do not interpret ANSI/
// VT100 escape sequences by default: the two bytes ESC [ and everything after
// them render as literal garbage characters rather than as colour. Windows
// Terminal and recent PowerShell already have this on; a default cmd.exe does
// not, unless a process asks for it.
//
// The standard library's own syscall package documents exactly this gap: it
// exports GetConsoleMode directly but not SetConsoleMode, and its own doc
// comment for LazyDLL says "use LazyDLL in golang.org/x/sys/windows for a
// secure way to load system DLLs" -- naming the dependency this project does
// not take. What syscall does still export on Windows are the primitives
// SetConsoleMode is built from: NewLazyDLL, NewProc and Call, which is enough
// to make the one Win32 call needed without leaving the standard library.
//
// This is best-effort and silent on failure by design. colorEnabled has
// already confirmed the destination is a real console via os.ModeCharDevice;
// if enabling virtual terminal processing fails anyway -- a console that
// predates the feature, or one already configured -- the worst outcome is
// escape codes not rendering, never corrupted output.

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
	enableANSIOnce     sync.Once
)

func enablePlatformANSI(f *os.File) {
	enableANSIOnce.Do(func() {
		handle := syscall.Handle(f.Fd())
		var mode uint32
		if err := syscall.GetConsoleMode(handle, &mode); err != nil {
			return // not a console syscall recognises, or output is redirected
		}
		_, _, _ = procSetConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	})
}
