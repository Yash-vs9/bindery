package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunExitCodes pins the CLI contract: a wrong command line is exit 2, a
// command that ran and failed is exit 1, success is 0. Shell scripts branch on
// these, so they are part of the public surface.
func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, exitUsage},
		{"unknown command", []string{"frobnicate"}, exitUsage},
		{"help", []string{"help"}, exitOK},
		{"help flag", []string{"--help"}, exitOK},
		{"version", []string{"version"}, exitOK},
		{"unknown flag", []string{"build", "-nope"}, exitUsage},
		{"render without a file", []string{"render"}, exitUsage},
		{"render with two files", []string{"render", "a.md", "b.md"}, exitUsage},
		{"failing command", []string{"build", "/nonexistent-directory-xyz"}, exitFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tt.args, &stdout, &stderr); got != tt.want {
				t.Errorf("run(%q) = %d, want %d\nstdout: %s\nstderr: %s",
					tt.args, got, tt.want, stdout.String(), stderr.String())
			}
		})
	}
}

// TestVersionGoesToStdout guards the stdout/stderr split: data on stdout,
// diagnostics on stderr, so that bindery composes with pipes.
// TestBuildWritesSite exercises the build command end to end.
//
// Every path here is inside t.TempDir(). An earlier version of this file ran
// "bindery build" with no arguments, which meant the test built the repository
// it lives in and wrote a site/ directory as a side effect. A test that touches
// the working tree is a test that will eventually be wrong about why it failed.
func TestBuildWritesSite(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "site")

	writeFile(t, filepath.Join(src, "index.md"), "# Home\n\nWelcome.\n")
	if err := os.MkdirAll(filepath.Join(src, "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "guide", "intro.md"), "# Intro\n\nText.\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", src, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("build exited %d: %s", code, stderr.String())
	}

	for _, want := range []string{"index.html", filepath.Join("guide", "intro.html")} {
		body, err := os.ReadFile(filepath.Join(out, want))
		if err != nil {
			t.Errorf("expected %s: %v", want, err)
			continue
		}
		if !strings.Contains(string(body), "<!DOCTYPE html>") {
			t.Errorf("%s is not a complete page", want)
		}
		// build must never inject the live-reload client; only dev does.
		if strings.Contains(string(body), "__bindery/live") {
			t.Errorf("%s contains the live-reload client", want)
		}
	}
}

// TestFlagsAfterPositional guards the argument permutation. Package flag stops
// at the first positional, so without permuting, --out here would be ignored and
// the site would land in the default directory.
func TestFlagsAfterPositional(t *testing.T) {
	src := t.TempDir()
	out := filepath.Join(t.TempDir(), "elsewhere")
	writeFile(t, filepath.Join(src, "index.md"), "# Home\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"build", src, "--out", out}, &stdout, &stderr); code != exitOK {
		t.Fatalf("build exited %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		t.Errorf("flag after positional was ignored: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVersionGoesToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("version exited %d, want %d", code, exitOK)
	}
	if !strings.HasPrefix(stdout.String(), "bindery "+version) {
		t.Errorf("stdout = %q, want it to start with the version", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", stderr.String())
	}
}

// TestErrorsGoToStderr checks the other half of that split.
func TestErrorsGoToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"build", "/nonexistent-directory-xyz"}, &stdout, &stderr)
	if code != exitFailure {
		t.Fatalf("build of a missing directory exited %d, want %d", code, exitFailure)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no such file or directory") {
		t.Errorf("stderr = %q, want it to explain the failure", stderr.String())
	}
}
