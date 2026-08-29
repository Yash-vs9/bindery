package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestServer builds a small site in a temporary directory and serves it.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.md"), "# Home\n\nWelcome to *docs*.\n")
	if err := os.MkdirAll(filepath.Join(dir, "guide"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "guide", "intro.md"), "# Intro\n\nText.\n")
	writeFile(t, filepath.Join(dir, "logo.svg"), `<svg xmlns="http://www.w3.org/2000/svg"/>`)

	srv, err := NewServer(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestServeRouting(t *testing.T) {
	_, ts := newTestServer(t)

	tests := []struct {
		path string
		want int
	}{
		{"/", http.StatusOK},
		{"/index.html", http.StatusOK},
		{"/guide/intro.html", http.StatusOK},
		{"/guide/intro", http.StatusOK}, // extensionless
		{"/logo.svg", http.StatusOK},    // a non-Markdown asset
		{"/missing.html", http.StatusNotFound},
		{"/guide/missing", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, err := ts.Client().Get(ts.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("GET %s = %d, want %d", tt.path, resp.StatusCode, tt.want)
			}
		})
	}
}

func TestServeInjectsLiveReloadOnlyInDev(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(body), "__bindery/live") {
		t.Error("dev server did not inject the live-reload client")
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store; a cached dev page defeats live reload", got)
	}
	if !strings.Contains(string(body), "<em>docs</em>") {
		t.Error("page body was not rendered from Markdown")
	}
}

func TestServeConditionalGET(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a page response")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", resp2.StatusCode)
	}
}

// TestServeRefusesTraversal sends a path the Go http client would normalise
// away, so the request has to be written by hand onto the socket.
func TestServeRefusesTraversal(t *testing.T) {
	_, ts := newTestServer(t)
	host := strings.TrimPrefix(ts.URL, "http://")

	for _, target := range []string{
		"/../go.mod",
		"/../../etc/passwd",
		"/%2e%2e/go.mod",
		"/guide/../../go.mod",
	} {
		t.Run(target, func(t *testing.T) {
			conn, err := net.Dial("tcp", host)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target, host)

			status, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(status, "200") {
				t.Errorf("GET %s returned 200; traversal was not refused", target)
			}
		})
	}
}

func TestServeRebuildPicksUpChanges(t *testing.T) {
	srv, ts := newTestServer(t)

	page := filepath.Join(srv.root, "index.md")
	writeFile(t, page, "# Home\n\nBrand new text.\n")
	if err := srv.Rebuild(); err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Brand new text.") {
		t.Error("rebuild did not replace the served site")
	}
}

// TestServeRebuildKeepsLastGoodBuild checks the failure policy: a source
// directory that stops being loadable must not blank the browser.
func TestServeRebuildKeepsLastGoodBuild(t *testing.T) {
	srv, ts := newTestServer(t)

	// Remove every Markdown file, which makes LoadSite fail.
	for _, p := range []string{"index.md", filepath.Join("guide", "intro.md")} {
		if err := os.Remove(filepath.Join(srv.root, p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.Rebuild(); err == nil {
		t.Fatal("Rebuild succeeded with no markdown files, want an error")
	}

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("after a failed rebuild, GET / = %d, want the last good build", resp.StatusCode)
	}
}

// TestWebSocketHandshakeAndBroadcast speaks the protocol on a raw socket: it
// sends a handshake by hand, checks the 101 response and the accept header, then
// has the hub broadcast and decodes the frame that arrives.
//
// This is the closest an automated test gets to "does a browser connect". The
// handshake arithmetic itself is pinned separately against the RFC's worked
// example in TestAcceptKey.
func TestWebSocketHandshakeAndBroadcast(t *testing.T) {
	srv, ts := newTestServer(t)
	host := strings.TrimPrefix(ts.URL, "http://")

	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	fmt.Fprintf(conn, "GET /__bindery/live HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", host, key)

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake status = %q, want 101 Switching Protocols", strings.TrimSpace(status))
	}

	headers := map[string]string{}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, _ := strings.Cut(line, ":")
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	if got, want := headers["sec-websocket-accept"], acceptKey(key); got != want {
		t.Errorf("Sec-WebSocket-Accept = %q, want %q", got, want)
	}
	if got := strings.ToLower(headers["upgrade"]); got != "websocket" {
		t.Errorf("Upgrade = %q, want websocket", got)
	}

	// Wait for the hub to register the client before broadcasting.
	deadline := time.Now().Add(2 * time.Second)
	for srv.hub.Clients() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if srv.hub.Clients() != 1 {
		t.Fatalf("hub has %d clients, want 1", srv.hub.Clients())
	}

	srv.hub.Broadcast("reload")

	opcode, payload, err := readTestFrame(br)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != opText {
		t.Errorf("opcode = %#x, want %#x (text)", opcode, opText)
	}
	if string(payload) != "reload" {
		t.Errorf("payload = %q, want %q", payload, "reload")
	}
}

// readTestFrame decodes a server-to-client frame, which per RFC 6455 must not
// be masked. Asserting that is part of the point: a masked server frame is a
// protocol violation and browsers close the connection over it.
func readTestFrame(br *bufio.Reader) (opcode byte, payload []byte, err error) {
	var header [2]byte
	if _, err = io.ReadFull(br, header[:]); err != nil {
		return 0, nil, err
	}
	if header[0]&0x80 == 0 {
		return 0, nil, fmt.Errorf("FIN not set; fragmented frames are not expected")
	}
	if header[1]&0x80 != 0 {
		return 0, nil, fmt.Errorf("server frame is masked, which RFC 6455 §5.1 forbids")
	}
	opcode = header[0] & 0x0F
	size := uint64(header[1] & 0x7F)
	switch size {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(br, ext[:]); err != nil {
			return 0, nil, err
		}
		size = binary.BigEndian.Uint64(ext[:])
	}
	payload = make([]byte, size)
	_, err = io.ReadFull(br, payload)
	return opcode, payload, err
}
