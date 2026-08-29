package main

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "hello", 5},
		{"empty", "", 0},
		{"ansi sequences are invisible", ansiBold + "hi" + ansiReset, 2},
		{"only ansi", ansiRed + ansiReset, 0},
		{"cjk is double width", "日本", 4},
		{"mixed", "a日b", 4},
		{"combining mark is zero width", "é", 1},
		{"fullwidth forms", "ＡＢ", 4},
		{"emoji is double width", "🎉", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayWidth(tt.in); got != tt.want {
				t.Errorf("displayWidth(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestRenderANSIWraps checks the property that matters for a terminal: no line
// exceeds the width it was given.
func TestRenderANSIWraps(t *testing.T) {
	long := strings.Repeat("word ", 60)
	out := RenderANSI(Parse(long), 40, false)
	for _, line := range strings.Split(out, "\n") {
		if displayWidth(line) > 40 {
			t.Errorf("line exceeds 40 columns (%d): %q", displayWidth(line), line)
		}
	}
}

func TestRenderANSIStructure(t *testing.T) {
	src := "# Title\n\n## Section\n\n- one\n- two\n\n> quoted\n\n```\ncode\n```\n\n---\n"
	out := RenderANSI(Parse(src), 60, false)

	for _, want := range []string{"TITLE", "Section", "• one", "│ quoted", "  code", "─"} {
		if !strings.Contains(out, want) {
			t.Errorf("ansi output is missing %q\n%s", want, out)
		}
	}
	// With colour off, no escape sequences may appear at all.
	if strings.Contains(out, "\x1b[") {
		t.Error("colour was disabled but escape sequences were emitted")
	}
}

func TestRenderANSIColour(t *testing.T) {
	out := RenderANSI(Parse("# Title\n"), 60, true)
	if !strings.Contains(out, "\x1b[") {
		t.Error("colour was enabled but no escape sequences were emitted")
	}
}

func FuzzRenderANSI(f *testing.F) {
	for _, seed := range []string{"# h", "- a\n- b", "> q", "```\nx\n```", "日本語", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		RenderANSI(Parse(src), 40, false)
	})
}
