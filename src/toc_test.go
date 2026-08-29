package main

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Getting started", "getting-started"},
		{"Getting Started (v2)", "getting-started-v2"},
		{"Hello!", "hello"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"multiple   spaces", "multiple-spaces"},
		{"already-hyphenated", "already-hyphenated"},
		{"under_scores", "under-scores"},
		{"dots.and.commas,", "dots-and-commas"},
		{"CamelCase", "camelcase"},
		{"numbers 123", "numbers-123"},
		{"C++ and C#", "c-and-c"},
		// Letters survive in any script, so a non-Latin heading gets a readable
		// anchor rather than an empty one.
		{"日本語の見出し", "日本語の見出し"},
		{"Привет мир", "привет-мир"},
		{"emoji 🎉 heading", "emoji-heading"},
		// A heading with nothing sluggable still needs an anchor.
		{"!!!", "section"},
		{"", "section"},
		{"   ", "section"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := slugify(tt.in); got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUniqueSlug covers the case that makes anchors actually work: two headings
// with the same text must not produce the same anchor, or one of them is
// unreachable.
func TestUniqueSlug(t *testing.T) {
	seen := map[string]int{}
	got := []string{
		uniqueSlug("usage", seen),
		uniqueSlug("usage", seen),
		uniqueSlug("usage", seen),
		uniqueSlug("other", seen),
		uniqueSlug("usage", seen),
	}
	want := []string{"usage", "usage-2", "usage-3", "other", "usage-4"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slug %d = %q, want %q", i, got[i], want[i])
		}
	}

	// A pre-existing "usage-2" must not be handed out twice.
	seen2 := map[string]int{}
	uniqueSlug("usage", seen2)
	uniqueSlug("usage-2", seen2)
	if third := uniqueSlug("usage", seen2); third == "usage-2" {
		t.Errorf("uniqueSlug reissued %q, which was already taken", third)
	}
}

func TestExtractHeadingsAssignsSlugs(t *testing.T) {
	doc := Parse("# Title\n\n## Usage\n\ntext\n\n### Deep\n\n## Usage\n")
	headings := extractHeadings(doc)

	if len(headings) != 4 {
		t.Fatalf("found %d headings, want 4", len(headings))
	}
	want := []string{"title", "usage", "deep", "usage-2"}
	for i, h := range headings {
		if h.Slug != want[i] {
			t.Errorf("heading %d slug = %q, want %q", i, h.Slug, want[i])
		}
	}

	// The block must carry the same slug the renderer will emit.
	for _, b := range doc.Root.Children {
		if b.Kind == KindHeading && b.slug == "" {
			t.Errorf("heading %q was not given a slug on the block", plainText(b.Inlines))
		}
	}

	// TOC keeps levels two and three only: level one is the page title, which
	// the sidebar already shows.
	toc := TOC(headings)
	if len(toc) != 3 {
		t.Errorf("TOC has %d entries, want 3 (levels 2 and 3 only)", len(toc))
	}
	for _, h := range toc {
		if h.Level < 2 || h.Level > 3 {
			t.Errorf("TOC contains a level-%d heading", h.Level)
		}
	}
}

func FuzzSlugify(f *testing.F) {
	for _, seed := range []string{"Getting Started", "!!!", "日本語", "a-b", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		slug := slugify(text)
		if slug == "" {
			t.Fatalf("slugify(%q) returned an empty anchor", text)
		}
		for _, r := range slug {
			if r == ' ' || r == '#' || r == '/' || r == '?' {
				t.Fatalf("slugify(%q) = %q, which contains %q and cannot be a fragment", text, slug, r)
			}
		}
	})
}
