package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The development server.
//
// Concurrency model, stated plainly because Track C asks for it and because it
// is the kind of thing that is easy to get quietly wrong:
//
//   - net/http serves each request on its own goroutine. Nothing here changes
//     that or tries to.
//   - The built site is immutable once constructed. A rebuild produces a whole
//     new *Site and swaps the pointer under a write lock; readers take a read
//     lock only long enough to copy the pointer, never for the duration of a
//     response. So a request that began before a rebuild finishes serves the
//     older site consistently rather than seeing half of each.
//   - The live-reload hub has its own locking, described in livereload.go.
//   - Rebuilds are serialised by the watcher, which is a single goroutine.
//
// There is deliberately no cache of rendered pages. Rendering a page is
// microseconds and a cache would need invalidating, which is a second source of
// truth about freshness in a program whose entire job is freshness.

// Server holds the state of a running dev server.
type Server struct {
	root string
	hub  *liveHub

	mu   sync.RWMutex
	site *Site

	// assets is a root-locked handle on the source directory, used to serve
	// non-Markdown files such as images. os.Root (Go 1.24) resolves paths
	// inside the directory and refuses to escape it, so a request for
	// "../../etc/passwd" cannot be satisfied no matter how it is encoded. Doing
	// this by hand with filepath.Clean and a prefix check is the classic way to
	// get path traversal wrong.
	assets *os.Root
}

// NewServer loads the site and prepares to serve it.
func NewServer(root string) (*Server, error) {
	site, err := LoadSite(root, true)
	if err != nil {
		return nil, err
	}
	assets, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &Server{root: root, hub: newLiveHub(), site: site, assets: assets}, nil
}

// Close releases the server's handle on the source directory.
func (s *Server) Close() error { return s.assets.Close() }

// Site returns the current site.
func (s *Server) Site() *Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.site
}

// Rebuild re-reads the source directory and replaces the served site.
//
// A parse or read failure leaves the previous site in place: a syntax error in
// one file should not blank the browser, it should be reported and ignored until
// the file is saved again.
func (s *Server) Rebuild() error {
	site, err := LoadSite(s.root, true)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.site = site
	s.mu.Unlock()
	return nil
}

// Handler returns the routing table.
//
// ServeMux gained method and wildcard patterns in Go 1.22, which is why there is
// no router here and no need for one. "GET /" is a catch-all that runs only when
// no more specific pattern matches, so the live-reload endpoint does not need to
// be special-cased inside the page handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__bindery/live", s.hub.Handle)
	mux.HandleFunc("GET /", s.servePage)
	return mux
}

// servePage serves a rendered page, or a file from the source directory.
func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	site := s.Site()
	urlPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))

	// A bare directory name gets the index page, with or without a trailing
	// slash, so that /guide and /guide/ both work.
	candidates := []string{urlPath}
	if ext := path.Ext(urlPath); ext == "" {
		candidates = append(candidates,
			strings.TrimSuffix(urlPath, "/")+".html",
			strings.TrimSuffix(urlPath, "/")+"/index.html",
		)
	}

	for _, candidate := range candidates {
		if page, ok := site.Page(candidate); ok {
			html, err := renderPage(site, page, true)
			if err != nil {
				http.Error(w, "render failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// A dev server must not be cached by the browser, or the live
			// reload reloads to a stale page.
			w.Header().Set("Cache-Control", "no-store")
			serveContent(w, r, []byte(html))
			return
		}
	}

	if s.serveAsset(w, r, urlPath) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, notFoundHTML, escapeHTML(urlPath))
}

// serveAsset serves a non-Markdown file from the source directory, and reports
// whether it did.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, urlPath string) bool {
	rel := strings.TrimPrefix(urlPath, "/")
	if rel == "" || isMarkdown(rel) {
		return false
	}
	f, err := s.assets.Open(filepath.FromSlash(rel))
	if err != nil {
		return false
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return false
	}

	if ct := contentType(rel); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	serveContent(w, r, body)
	return true
}

// serveContent writes a body with an ETag, answering a conditional request with
// 304 when the client already has it. A reload of an unchanged page then costs
// one round trip and no bytes.
func serveContent(w http.ResponseWriter, r *http.Request, body []byte) {
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)

	if match := r.Header.Get("If-None-Match"); match != "" {
		for _, candidate := range strings.Split(match, ",") {
			if strings.TrimSpace(candidate) == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// contentType returns a MIME type for a path.
//
// mime.TypeByExtension consults the system's MIME database, which varies between
// machines and is missing entries a documentation site needs. The explicit map
// comes first so that behaviour does not depend on what happens to be installed.
func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	}
	return mime.TypeByExtension(path.Ext(name))
}

const notFoundHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>404</title>
<style>body{font:16px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
margin:0;display:grid;place-items:center;min-height:100vh;background:#16171a;color:#e6e6e8}
code{background:#1e1f24;padding:.2em .4em;border-radius:4px}</style></head>
<body><div><h1>404</h1><p>No page at <code>%s</code>.</p></div></body></html>
`

// Serve runs the HTTP server until ctx is cancelled.
//
// The listener is created before returning so that the caller can report the
// real port -- which matters when the requested one is taken and the OS picks
// another, and when port 0 is passed deliberately, as the tests do.
func (s *Server) Serve(ctx context.Context, addr string) (*net.TCPAddr, func() error, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	srv := &http.Server{
		Handler: s.Handler(),
		// No write timeout: a live-reload socket is a long-lived connection and
		// a write deadline on the server would sever it. Frame writes carry
		// their own deadline instead, set in liveClient.writeFrame.
		ReadHeaderTimeout: 10 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()

	wait := func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return <-done
	}
	return ln.Addr().(*net.TCPAddr), wait, nil
}
