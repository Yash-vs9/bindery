// Package tests holds end-to-end tests: they build the binary and drive it the
// way a user does, through its command line and over HTTP.
//
// Why this directory exists alongside the unit tests in src/. Go requires
// _test.go files to sit in the same package as the code they test, and the unit
// tests exercise unexported identifiers -- parseBlocks, acceptKey, tokenise,
// slugify -- which no separate package can reach without exporting the entire
// internal surface. So the unit tests live in src/ where the language puts them.
//
// What belongs here is the layer those cannot cover: that the documented build
// command produces a working artifact, that exit codes are what the README
// promises, that the dev server actually serves and actually reloads, and that
// two builds of the same input produce the same bytes. These tests know nothing
// about the internals; if every one of them passes, the tool works.
package tests

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// binary is the compiled bindery, built once for the whole package.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bindery-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: cannot create temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	// Windows requires the .exe extension to execute a file at all --
	// exec.Command on a path without it fails with "executable file not
	// found in %PATH%" even though the file exists. This was found by a
	// full wall of failures on windows-latest in CI, none of them a real
	// bug in bindery itself: every single test in this package failed
	// identically, because none of them could launch the binary at all.
	binaryName := "bindery"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary = filepath.Join(dir, binaryName)
	build := exec.Command("go", "build", "-o", binary, "./src")
	build.Dir = ".."
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build failed:", err)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// run executes bindery and returns its output and exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return out.String(), errBuf.String(), code
}

// fixture writes a small documentation tree and returns its path.
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Built by concatenation rather than as a raw string literal: the fixture
	// contains backticks, and Go raw strings are backtick-delimited.
	write(t, filepath.Join(dir, "index.md"), strings.Join([]string{
		"---",
		"title: Home",
		"order: 1",
		"---",
		"# Home",
		"",
		"Welcome. This mentions *parsing* and a [link](https://example.com).",
		"",
		"## Details",
		"",
		"Some prose about parsers, with `code` in it.",
		"",
		"- one",
		"- two",
		"",
	}, "\n"))
	if err := os.MkdirAll(filepath.Join(dir, "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "guide", "intro.md"), "# Intro\n\n```go\nfunc main() {}\n```\n")
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- the documented command line -------------------------------------------

func TestVersion(t *testing.T) {
	stdout, _, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.HasPrefix(stdout, "bindery ") {
		t.Errorf("stdout = %q, want it to start with the tool name", stdout)
	}
}

// TestExitCodes pins the contract the README documents: 0 success, 1 the
// command ran and failed, 2 the command line was wrong.
func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, 2},
		{"unknown command", []string{"frobnicate"}, 2},
		{"unknown flag", []string{"build", "-nope"}, 2},
		{"render without a file", []string{"render"}, 2},
		{"unknown render format", []string{"render", "x.md", "--format", "toml"}, 2},
		{"help", []string{"help"}, 0},
		{"version", []string{"version"}, 0},
		{"build a directory that does not exist", []string{"build", "/nonexistent-xyz"}, 1},
		{"render a file that does not exist", []string{"render", "/nonexistent-xyz.md"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, code := run(t, tt.args...); code != tt.want {
				t.Errorf("bindery %v exited %d, want %d", tt.args, code, tt.want)
			}
		})
	}
}

// TestDiagnosticsGoToStderr checks that bindery composes with pipes: data on
// stdout, complaints on stderr.
func TestDiagnosticsGoToStderr(t *testing.T) {
	stdout, stderr, code := run(t, "build", "/nonexistent-xyz")
	if code == 0 {
		t.Fatal("building a missing directory succeeded")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty on failure", stdout)
	}
	if stderr == "" {
		t.Error("stderr is empty; the failure was not explained")
	}
}

// --- building a site --------------------------------------------------------

func TestBuildProducesAStaticSite(t *testing.T) {
	src := fixture(t)
	out := filepath.Join(t.TempDir(), "site")

	stdout, stderr, code := run(t, "build", src, "--out", out)
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "2 pages") {
		t.Errorf("stdout = %q, want it to report two pages", stdout)
	}

	for _, want := range []string{"index.html", "guide/intro.html", "search-index.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(want))); err != nil {
			t.Errorf("expected %s in the output: %v", want, err)
		}
	}

	home := read(t, filepath.Join(out, "index.html"))
	for _, want := range []string{"<!DOCTYPE html>", "<em>parsing</em>", "<ul>", "<li>one</li>"} {
		if !strings.Contains(home, want) {
			t.Errorf("index.html is missing %q", want)
		}
	}
	// Front matter must win over the level-one heading.
	if !strings.Contains(home, "<title>Home</title>") {
		t.Error("front-matter title was not used")
	}
	// A published site carries no live-reload client.
	if strings.Contains(home, "__bindery/live") {
		t.Error("build output contains the live-reload client")
	}

	guide := read(t, filepath.Join(out, "guide", "intro.html"))
	if !strings.Contains(guide, `class="hl-kw"`) {
		t.Error("fenced go code was not highlighted")
	}
}

// TestBuildIsReproducible is the claim the README makes, tested end to end
// rather than only for the binary.
func TestBuildIsReproducible(t *testing.T) {
	src := fixture(t)
	base := t.TempDir()
	first, second := filepath.Join(base, "a"), filepath.Join(base, "b")

	for _, out := range []string{first, second} {
		if _, stderr, code := run(t, "build", src, "--out", out); code != 0 {
			t.Fatalf("build exited %d: %s", code, stderr)
		}
	}

	err := filepath.Walk(first, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(first, path)
		a, b := read(t, path), read(t, filepath.Join(second, rel))
		if a != b {
			t.Errorf("%s differs between two builds of identical input", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- rendering --------------------------------------------------------------

func TestRenderFormats(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.md")
	write(t, page, "# Title\n\nSome *emphasis* and `code`.\n")

	t.Run("html", func(t *testing.T) {
		stdout, _, code := run(t, "render", page, "--format", "html")
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(stdout, "<h1>Title</h1>") || !strings.Contains(stdout, "<em>emphasis</em>") {
			t.Errorf("html output looks wrong: %q", stdout)
		}
	})

	t.Run("json", func(t *testing.T) {
		stdout, _, code := run(t, "render", page, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		for _, want := range []string{`"Kind": "Document"`, `"Kind": "Heading"`, `"Kind": "Emph"`} {
			if !strings.Contains(stdout, want) {
				t.Errorf("json output is missing %s", want)
			}
		}
	})

	t.Run("ansi honours NO_COLOR", func(t *testing.T) {
		cmd := exec.Command(binary, "render", page, "--format", "ansi")
		cmd.Env = append(os.Environ(), "NO_COLOR=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(out, []byte("\x1b[")) {
			t.Error("NO_COLOR was set but escape sequences were emitted")
		}
		if !bytes.Contains(out, []byte("TITLE")) {
			t.Errorf("ansi output looks wrong: %q", out)
		}
	})
}

// --- conformance ------------------------------------------------------------

// TestSpecReportsConformance runs the CommonMark suite through the CLI and
// checks the number the README publishes is the number the tool reports.
//
// This asserts full conformance -- 652 of 652. It has not always: for two
// milestones this pinned 651/652 deliberately, so that reaching 652 would break
// the build and force README.md to be updated in the same commit rather than
// drifting from what the tool actually does. See casefold.go and the git
// history for how the last example was closed.
func TestSpecReportsConformance(t *testing.T) {
	stdout, _, code := run(t, "spec")
	if code != 0 {
		t.Fatalf("bindery spec exited %d:\n%s", code, stdout)
	}

	scoreLine := regexp.MustCompile(`(\d+)/(\d+) \(([\d.]+)%\)`).FindStringSubmatch(stdout)
	if scoreLine == nil {
		t.Fatalf("spec output has no score line:\n%s", stdout)
	}
	t.Logf("conformance: %s of %s examples (%s%%)", scoreLine[1], scoreLine[2], scoreLine[3])

	if scoreLine[2] != "652" {
		t.Errorf("suite size = %s, want 652 (CommonMark 0.31.2)", scoreLine[2])
	}
	if scoreLine[1] != "652" {
		t.Errorf("passing = %s, want 652 (full conformance); the README publishes 652/652", scoreLine[1])
	}
	if code != 0 {
		t.Errorf("full conformance should exit 0, got %d", code)
	}
}

// --- the dev server ---------------------------------------------------------

// TestDevServerServesAndReloads starts the real server on a port the OS picks,
// serves a page, edits a source file, and waits for the change to appear. It is
// the closest an automated test gets to the demo.
func TestDevServerServesAndReloads(t *testing.T) {
	src := fixture(t)

	cmd := exec.Command(binary, "dev", src, "--port", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// The server prints the address it actually bound, which is how a test can
	// use port 0 and still know where to connect.
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`http://localhost:(\d+)`).FindStringSubmatch(line)
	if match == nil {
		t.Fatalf("server did not announce an address: %q", line)
	}
	base := "http://localhost:" + match[1]
	go io.Copy(io.Discard, reader)

	body := getWithin(t, base+"/", 5*time.Second)
	if !strings.Contains(body, "Welcome.") {
		t.Fatalf("served page is missing its content: %.200s", body)
	}
	if !strings.Contains(body, "__bindery/live") {
		t.Error("dev server did not inject the live-reload client")
	}

	// The search index is served, not just written by build.
	if index := getWithin(t, base+"/search-index.json", 5*time.Second); !strings.Contains(index, `"docs"`) {
		t.Errorf("search index looks wrong: %.200s", index)
	}

	// Edit a source file; the watcher should pick it up and the server should
	// serve the new content without a restart.
	write(t, filepath.Join(src, "index.md"), "---\ntitle: Home\n---\n# Home\n\nEntirely new prose.\n")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(getWithin(t, base+"/", 5*time.Second), "Entirely new prose.") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("edited file was not picked up by the watcher within ten seconds")
}

// getWithin fetches a URL, retrying until the server is listening.
func getWithin(t *testing.T, url string, limit time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		resp, err := http.Get(url)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s never succeeded: %v", url, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPDFCommand drives "bindery pdf" the way a user does and checks the file
// it leaves on disk is one a reader will open.
func TestPDFCommand(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "index.md"), "# Handbook\n\nBody text.\n\n- one\n- two\n")
	write(t, filepath.Join(dir, "guide.md"), "# Guide\n\n```go\nfunc main() {}\n```\n")
	out := filepath.Join(t.TempDir(), "handbook.pdf")

	stdout, stderr, code := run(t, "pdf", dir, "--out", out, "--title", "Handbook")
	if code != 0 {
		t.Fatalf("bindery pdf exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "handbook.pdf") {
		t.Errorf("stdout did not name the output file: %q", stdout)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no PDF was written: %v", err)
	}
	if len(body) < 1000 {
		t.Errorf("PDF is %d bytes, which is too small to contain the input", len(body))
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Error("output is not a PDF")
	}
	if !bytes.HasSuffix(body, []byte("%%EOF\n")) {
		t.Error("PDF is truncated")
	}
	// Two pages of content plus a cover.
	if pages := bytes.Count(body, []byte("/Type /Page ")); pages != 3 {
		t.Errorf("got %d pages, want 3 (cover plus two documents)", pages)
	}
}

// TestPDFCommandIsReproducible checks the claim end to end rather than in a
// unit test: the same input, through the real binary, twice.
func TestPDFCommandIsReproducible(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "index.md"), "# T\n\nText.\n")

	outDir := t.TempDir()
	first := filepath.Join(outDir, "a.pdf")
	second := filepath.Join(outDir, "b.pdf")
	for _, out := range []string{first, second} {
		if _, stderr, code := run(t, "pdf", dir, "--out", out); code != 0 {
			t.Fatalf("bindery pdf exited %d: %s", code, stderr)
		}
	}

	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two runs produced different PDFs")
	}
}

// TestCrossCompileMatrix builds every platform "make release" publishes and
// checks each is a plausible binary for its target. It cannot run any of them
// except the one matching the host -- that is what the CI matrix in
// verify.yml is for, building and testing natively on Linux, macOS and
// Windows -- but a cross-compiled binary that is truncated, empty, or was
// silently skipped is still a failure this test catches on any one platform.
func TestCrossCompileMatrix(t *testing.T) {
	platforms := []struct{ os, arch string }{
		{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"},
	}

	for _, p := range platforms {
		t.Run(p.os+"_"+p.arch, func(t *testing.T) {
			ext := ""
			if p.os == "windows" {
				ext = ".exe"
			}
			out := filepath.Join(t.TempDir(), "bindery"+ext)

			cmd := exec.Command("go", "build", "-o", out, "./src")
			cmd.Dir = ".."
			cmd.Env = append(os.Environ(),
				"GOOS="+p.os, "GOARCH="+p.arch, "CGO_ENABLED=0")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("cross-compile for %s/%s failed: %v\n%s", p.os, p.arch, err, stderr.String())
			}

			info, err := os.Stat(out)
			if err != nil {
				t.Fatalf("no binary produced for %s/%s: %v", p.os, p.arch, err)
			}
			// An empty or near-empty file means the build silently produced
			// nothing useful rather than failing loudly.
			if info.Size() < 1_000_000 {
				t.Errorf("%s/%s binary is %d bytes, too small to be real", p.os, p.arch, info.Size())
			}
		})
	}
}
