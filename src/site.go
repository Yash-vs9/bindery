package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Site discovery and building.
//
// A site is a directory of Markdown files. Discovery walks it once, parses each
// file, and derives the navigation from what it found -- there is no
// configuration file, because the directory structure already says what the
// author meant.

// Page is one source file and everything derived from it.
type Page struct {
	SourcePath string // path on disk
	RelPath    string // path relative to the site root, e.g. "guide/intro.md"
	URL        string // URL path, e.g. "/guide/intro.html"
	OutPath    string // path relative to the output directory
	Title      string
	Doc        *Document
	Body       string // rendered HTML fragment
	Headings   []Heading

	Meta  map[string]any // front matter, if any
	Order int            // "order:" in front matter; sorts the navigation
	Draft bool           // "draft: true"; served by dev, omitted by build
}

// Site is a parsed directory of Markdown.
type Site struct {
	Root  string
	Pages []*Page
}

// LoadSite walks root, parses every Markdown file, and returns the site.
//
// Ordering is deterministic: WalkDir visits lexically, and the navigation is
// sorted explicitly afterwards. That matters for the reproducible-build claim,
// which would be worthless if page order depended on the filesystem.
func LoadSite(root string, includeDrafts bool) (*Site, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	site := &Site{Root: root}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		// Skip dotfiles, and skip the conventional output directory so that
		// building into the source tree does not feed on its own output.
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "site" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !isMarkdown(name) {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		page, err := newPage(p, filepath.ToSlash(rel), string(src))
		if err != nil {
			// Name the file. A front-matter error carries a line and column,
			// which are useless without knowing which file they are in.
			return fmt.Errorf("%s: %w", rel, err)
		}
		if page.Draft && !includeDrafts {
			return nil
		}
		site.Pages = append(site.Pages, page)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(site.Pages) == 0 {
		return nil, fmt.Errorf("no markdown files found in %s", root)
	}

	sort.Slice(site.Pages, func(i, j int) bool {
		a, b := site.Pages[i], site.Pages[j]
		// The home page sorts first, then anything with an explicit "order:" in
		// front matter, then everything else alphabetically by URL.
		if x, y := a.URL == "/index.html", b.URL == "/index.html"; x != y {
			return x
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.URL < b.URL
	})
	return site, nil
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// newPage parses one source file and derives its URL, title and metadata.
func newPage(sourcePath, rel, src string) (*Page, error) {
	meta, body, err := splitFrontMatter(src)
	if err != nil {
		return nil, err
	}
	doc := ParseWithOptions(body, ParseOptions{Tables: true})

	// README.md and index.md both become the directory's index page, which is
	// what makes bindery work on a repository you did not write for it.
	dir, base := path.Split(rel)
	stem := strings.TrimSuffix(base, path.Ext(base))
	out := dir + stem + ".html"
	if lower := strings.ToLower(stem); lower == "readme" || lower == "index" {
		out = dir + "index.html"
	}

	// Headings are extracted first: doing so assigns the slugs that the
	// renderer emits as heading anchors, so the order is not incidental.
	headings := extractHeadings(doc)

	title := pageTitle(doc, stem)
	if fromMeta, ok := stringValue(meta, "title"); ok && fromMeta != "" {
		title = fromMeta
	}

	// Ordering defaults to a large number so that pages without an explicit
	// "order:" sort after those with one, rather than jumping to the front.
	const unordered = 1 << 30

	return &Page{
		SourcePath: sourcePath,
		RelPath:    rel,
		URL:        "/" + out,
		OutPath:    out,
		Title:      title,
		Doc:        doc,
		Headings:   headings,
		Meta:       meta,
		Order:      intValue(meta, "order", unordered),
		Draft:      boolValue(meta, "draft", false),
		Body:       RenderHTMLWith(doc, RenderOptions{Highlight: true, HeadingIDs: true, Diagrams: true}),
	}, nil
}

// pageTitle returns the first level-one heading, falling back to the filename
// with separators turned back into spaces.
func pageTitle(doc *Document, stem string) string {
	for _, b := range doc.Root.Children {
		if b.Kind == KindHeading && b.Level == 1 {
			if t := strings.TrimSpace(plainText(b.Inlines)); t != "" {
				return t
			}
		}
	}
	return strings.ReplaceAll(strings.ReplaceAll(stem, "-", " "), "_", " ")
}

// Nav returns the navigation entries for the sidebar.
func (s *Site) Nav(current string) []NavEntry {
	entries := make([]NavEntry, 0, len(s.Pages))
	for _, p := range s.Pages {
		entries = append(entries, NavEntry{
			Title:   p.Title,
			URL:     p.URL,
			Current: p.URL == current,
		})
	}
	return entries
}

// NavEntry is one sidebar link.
type NavEntry struct {
	Title   string
	URL     string
	Current bool
}

// Page returns the page served at a URL path, and whether one exists.
func (s *Site) Page(urlPath string) (*Page, bool) {
	if urlPath == "/" || urlPath == "" {
		urlPath = "/index.html"
	}
	if strings.HasSuffix(urlPath, "/") {
		urlPath += "index.html"
	}
	for _, p := range s.Pages {
		if p.URL == urlPath {
			return p, true
		}
	}
	return nil, false
}

// SearchIndexName is where the search index is written and fetched from.
const SearchIndexName = "search-index.json"

// Build writes the whole site to outDir.
func (s *Site) Build(outDir string, live bool) error {
	index, err := BuildSearchIndex(s).JSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, SearchIndexName), index, 0o644); err != nil {
		return err
	}

	for _, p := range s.Pages {
		target := filepath.Join(outDir, filepath.FromSlash(p.OutPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		html, err := renderPage(s, p, live)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(html), 0o644); err != nil {
			return err
		}
	}
	return nil
}
