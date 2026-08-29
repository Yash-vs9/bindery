package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Watcher tests run inside a testing/synctest bubble, where the clock is
// virtual: time.Sleep and time.Ticker advance instantly once every goroutine in
// the bubble is blocked. That turns what would otherwise be a sleep-and-hope
// test -- the flakiest kind there is -- into a deterministic one, and replaces
// what jest.useFakeTimers or sinon would provide elsewhere.
//
// testing/synctest became stable in Go 1.25. synctest.Run is deprecated in
// favour of synctest.Test.

// TestWatcherDebounceCoalescesBurst proves the debounce does its job: three
// changes arriving faster than the debounce interval produce exactly one
// rebuild, not three.
func TestWatcherDebounceCoalescesBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		page := filepath.Join(dir, "page.md")
		write(t, page, "# one")

		var rebuilds atomic.Int32
		w := &Watcher{
			Root:     dir,
			Poll:     10 * time.Millisecond,
			Debounce: 100 * time.Millisecond,
			OnChange: func() { rebuilds.Add(1) },
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go w.Run(ctx)

		// Let the initial scan settle. It must not count as a change.
		time.Sleep(50 * time.Millisecond)
		if got := rebuilds.Load(); got != 0 {
			t.Fatalf("rebuilds after initial scan = %d, want 0", got)
		}

		// Three saves, each arriving before the debounce could fire.
		for i, content := range []string{"# two", "# three long", "# four longer"} {
			write(t, page, content)
			time.Sleep(30 * time.Millisecond)
			if got := rebuilds.Load(); got != 0 {
				t.Fatalf("rebuild fired mid-burst after save %d (count %d)", i+1, got)
			}
		}

		// Once the tree goes quiet, exactly one rebuild.
		time.Sleep(200 * time.Millisecond)
		if got := rebuilds.Load(); got != 1 {
			t.Errorf("rebuilds after burst = %d, want 1", got)
		}

		cancel()
		synctest.Wait()
	})
}

// TestWatcherDetectsCreateAndDelete covers the two changes a size-and-mtime
// comparison could plausibly miss.
func TestWatcherDetectsCreateAndDelete(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "a.md"), "# a")

		var rebuilds atomic.Int32
		w := &Watcher{
			Root:     dir,
			Poll:     10 * time.Millisecond,
			Debounce: 20 * time.Millisecond,
			OnChange: func() { rebuilds.Add(1) },
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go w.Run(ctx)
		time.Sleep(50 * time.Millisecond)

		newFile := filepath.Join(dir, "b.md")
		write(t, newFile, "# b")
		time.Sleep(100 * time.Millisecond)
		if got := rebuilds.Load(); got != 1 {
			t.Fatalf("rebuilds after create = %d, want 1", got)
		}

		if err := os.Remove(newFile); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if got := rebuilds.Load(); got != 2 {
			t.Errorf("rebuilds after delete = %d, want 2", got)
		}

		cancel()
		synctest.Wait()
	})
}

// TestWatcherIgnoresNonMarkdown checks that the walk filter holds: editing a
// file bindery does not read must not trigger a rebuild.
func TestWatcherIgnoresNonMarkdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "page.md"), "# page")

		var rebuilds atomic.Int32
		w := &Watcher{
			Root:     dir,
			Poll:     10 * time.Millisecond,
			Debounce: 20 * time.Millisecond,
			OnChange: func() { rebuilds.Add(1) },
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go w.Run(ctx)
		time.Sleep(50 * time.Millisecond)

		write(t, filepath.Join(dir, "notes.txt"), "not markdown")
		write(t, filepath.Join(dir, ".hidden.md"), "# hidden")
		time.Sleep(100 * time.Millisecond)

		if got := rebuilds.Load(); got != 0 {
			t.Errorf("rebuilds = %d, want 0", got)
		}

		cancel()
		synctest.Wait()
	})
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAcceptKey pins the handshake against the worked example in RFC 6455
// section 1.3. If this breaks, no browser will complete the handshake, and the
// failure mode is silent -- the socket simply never opens.
//
// The RFC publishes the intermediate SHA-1 digest as well as the final accept
// value, so both are asserted. Checking the digest separately says whether a
// failure is in the hashing or in the base64 encoding.
func TestAcceptKey(t *testing.T) {
	const (
		key        = "dGhlIHNhbXBsZSBub25jZQ=="
		wantDigest = "b37a4f2cc0624f1690f64606cf385945b2bec4ea"
		wantAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	)
	sum := sha1.Sum([]byte(key + websocketGUID))
	if got := hex.EncodeToString(sum[:]); got != wantDigest {
		t.Errorf("sha1(key+GUID) = %s, want %s (RFC 6455 §1.3)", got, wantDigest)
	}
	if got := acceptKey(key); got != wantAccept {
		t.Errorf("acceptKey(%q) = %q, want %q (RFC 6455 §1.3)", key, got, wantAccept)
	}
}

func TestHeaderContainsToken(t *testing.T) {
	tests := []struct {
		header, token string
		want          bool
	}{
		{"Upgrade", "upgrade", true},
		{"keep-alive, Upgrade", "upgrade", true},
		{"Upgrade, keep-alive", "upgrade", true},
		{"UPGRADE", "upgrade", true},
		{"keep-alive", "upgrade", false},
		{"", "upgrade", false},
		{"upgraded", "upgrade", false},
	}
	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.header, " ", "_"), func(t *testing.T) {
			if got := headerContainsToken(tt.header, tt.token); got != tt.want {
				t.Errorf("headerContainsToken(%q, %q) = %v, want %v",
					tt.header, tt.token, got, tt.want)
			}
		})
	}
}
