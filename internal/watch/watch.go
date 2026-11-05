// Package watch delivers change notifications for Claude Code profile
// directories without polling.
package watch

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Rescan is sent in a batch when the OS dropped events and everything should
// be re-read.
const Rescan = ""

// coalesce is how long events are gathered before a batch is delivered.
const coalesce = 250 * time.Millisecond

// Watcher watches whole profile directories (recursively) and emits batches
// of changed paths that matter: *.jsonl transcripts and .credentials.json.
type Watcher struct {
	Events chan []string

	raw  chan string
	mu   sync.Mutex
	dirs map[string]func() // lower(dir) -> stop
}

func New() *Watcher {
	w := &Watcher{
		Events: make(chan []string, 1),
		raw:    make(chan string, 256),
		dirs:   map[string]func(){},
	}
	go w.batch()
	return w
}

func (w *Watcher) Add(dir string) error {
	key := strings.ToLower(dir)
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.dirs[key]; ok {
		return nil
	}
	stop, err := watchTree(dir, w.emit)
	if err != nil {
		return err
	}
	w.dirs[key] = stop
	return nil
}

func (w *Watcher) Remove(dir string) {
	key := strings.ToLower(dir)
	w.mu.Lock()
	defer w.mu.Unlock()
	if stop, ok := w.dirs[key]; ok {
		stop()
		delete(w.dirs, key)
	}
}

func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, stop := range w.dirs {
		stop()
		delete(w.dirs, k)
	}
}

// Relevant reports whether a changed path is worth reacting to.
func Relevant(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".jsonl") ||
		strings.EqualFold(filepath.Base(p), ".credentials.json")
}

func (w *Watcher) emit(p string) {
	if p != Rescan && !Relevant(p) {
		return
	}
	select {
	case w.raw <- p:
	default: // buffer full: ask for a rescan instead of blocking the OS reader
		select {
		case w.raw <- Rescan:
		default:
		}
	}
}

func (w *Watcher) batch() {
	pending := map[string]struct{}{}
	var timer <-chan time.Time
	for {
		select {
		case p := <-w.raw:
			pending[p] = struct{}{}
			if timer == nil {
				timer = time.After(coalesce)
			}
		case <-timer:
			out := make([]string, 0, len(pending))
			for p := range pending {
				out = append(out, p)
			}
			pending = map[string]struct{}{}
			timer = nil
			w.Events <- out
		}
	}
}
