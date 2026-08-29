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
func LoadSite(root string) (*Site, error) {
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
		site.Pages = append(site.Pages, newPage(p, filepath.ToSlash(rel), string(src)))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(site.Pages) == 0 {
		return nil, fmt.Errorf("no markdown files found in %s", root)
	}

	sort.Slice(site.Pages, func(i, j int) bool {
		// The home page sorts first; everything else alphabetically by URL.
		if a, b := site.Pages[i].URL == "/index.html", site.Pages[j].URL == "/index.html"; a != b {
			return a
		}
		return site.Pages[i].URL < site.Pages[j].URL
	})
	return site, nil
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// newPage parses one source file and derives its URL and title.
func newPage(sourcePath, rel, src string) *Page {
	doc := Parse(src)

	// README.md and index.md both become the directory's index page, which is
	// what makes bindery work on a repository you did not write for it.
	dir, base := path.Split(rel)
	stem := strings.TrimSuffix(base, path.Ext(base))
	out := dir + stem + ".html"
	if lower := strings.ToLower(stem); lower == "readme" || lower == "index" {
		out = dir + "index.html"
	}

	return &Page{
		SourcePath: sourcePath,
		RelPath:    rel,
		URL:        "/" + out,
		OutPath:    out,
		Title:      pageTitle(doc, stem),
		Doc:        doc,
		Body:       RenderHTML(doc),
	}
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

// Build writes the whole site to outDir.
func (s *Site) Build(outDir string, live bool) error {
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
