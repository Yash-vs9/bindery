package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// File watching.
//
// This is the largest hole in Go's standard library for a tool like this: there
// is no watch API. No inotify, no FSEvents, no kqueue wrapper -- nothing. The
// standard-library answer is to poll, so bindery polls, and says so in its
// README rather than letting a reader assume otherwise.
//
// Polling has one advantage worth naming: it cannot miss a change and it cannot
// deliver a spurious one, which the platform notification APIs both manage to do
// under editor save patterns that write to a temporary file and rename over the
// original. The cost is latency bounded by the poll interval, and a small
// constant amount of stat traffic.

// Watcher polls a directory tree and reports settled changes.
type Watcher struct {
	Root     string
	Poll     time.Duration // how often to stat the tree
	Debounce time.Duration // how long to wait for changes to stop
	OnChange func()

	seen map[string]fileState
}

type fileState struct {
	size int64
	mod  time.Time
}

// DefaultWatcher returns a watcher with intervals tuned for an editor: fast
// enough that a save feels immediate, slow enough not to register on a CPU
// graph.
func DefaultWatcher(root string, onChange func()) *Watcher {
	return &Watcher{
		Root:     root,
		Poll:     250 * time.Millisecond,
		Debounce: 120 * time.Millisecond,
		OnChange: onChange,
	}
}

// Run polls until ctx is cancelled.
//
// Changes are debounced rather than acted on directly. A single editor save can
// produce several filesystem events -- write, truncate, rename, chmod -- and a
// "Save All" produces a burst across many files. Rebuilding once per event would
// mean rebuilding the site four times and reloading the browser four times, so
// the debounce timer is reset by every change and only fires once the tree has
// been quiet for Debounce.
func (w *Watcher) Run(ctx context.Context) {
	w.seen = w.scan() // the initial state is not a change

	ticker := time.NewTicker(w.Poll)
	defer ticker.Stop()

	var (
		settle  *time.Timer
		settleC <-chan time.Time
	)
	defer func() {
		if settle != nil {
			settle.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			current := w.scan()
			if !sameTree(w.seen, current) {
				w.seen = current
				if settle == nil {
					settle = time.NewTimer(w.Debounce)
				} else {
					settle.Reset(w.Debounce)
				}
				settleC = settle.C
			}

		case <-settleC:
			settleC = nil
			w.OnChange()
		}
	}
}

// scan stats every Markdown file under the root.
func (w *Watcher) scan() map[string]fileState {
	tree := make(map[string]fileState)
	_ = filepath.WalkDir(w.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A file that vanished mid-walk is itself a change; leaving it out
			// of the map records that.
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != w.Root && (strings.HasPrefix(name, ".") || name == "site" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !isMarkdown(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		tree[p] = fileState{size: info.Size(), mod: info.ModTime()}
		return nil
	})
	return tree
}

// sameTree compares two scans. Size and modification time together catch every
// edit a text editor makes; a same-size same-timestamp change is possible in
// principle and not worth hashing every file to catch.
func sameTree(a, b map[string]fileState) bool {
	if len(a) != len(b) {
		return false
	}
	for path, sa := range a {
		sb, ok := b[path]
		if !ok || sa.size != sb.size || !sa.mod.Equal(sb.mod) {
			return false
		}
	}
	return true
}
