// Package logs tallies token usage from Claude Code session transcripts
// (<profile>/projects/**/*.jsonl), reading each file incrementally.
package logs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// keep must exceed the 5h session window so the whole window is covered.
const keep = 6 * time.Hour

type entry struct {
	ts              time.Time
	in, out, cw, cr int64
}

// Stats is the token tally for a time window.
type Stats struct {
	Requests     int
	Input        int64 // uncached input
	Output       int64
	CacheWrite   int64
	CacheRead    int64
	LastActivity time.Time
	// Spark holds billable tokens (input+output+cache writes) per
	// SparkBucket since the window start.
	Spark [SparkBuckets]int64
}

const (
	SparkBuckets = 30
	SparkBucket  = 10 * time.Minute // 30 x 10m = the 5h session window
)

type Tracker struct {
	mu      sync.Mutex
	root    string
	extra   string            // another projects dir to read from (filtered)
	keep    func(string) bool // which transcripts belong to this profile
	offsets map[string]int64
	entries map[string]entry
	last    time.Time
}

func New(profileDir string) *Tracker {
	return &Tracker{
		root:    filepath.Join(profileDir, "projects"),
		offsets: map[string]int64{},
		entries: map[string]entry{},
	}
}

// SetFilter also reads transcripts from extraProjects and keeps only the
// files for which keep returns true (in both directories).
func (t *Tracker) SetFilter(extraProjects string, keep func(path string) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !strings.EqualFold(filepath.Clean(extraProjects), filepath.Clean(t.root)) {
		t.extra = extraProjects
	}
	t.keep = keep
}

// Scan walks all transcripts. Files untouched within the keep window are only
// remembered by size so later appends are read from there.
func (t *Tracker) Scan() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, root := range []string{t.root, t.extra} {
		if root != "" {
			t.scanLocked(root)
		}
	}
}

func (t *Tracker) scanLocked(root string) {
	cutoff := time.Now().Add(-keep)
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		if t.keep != nil && !t.keep(p) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if _, ok := t.offsets[p]; !ok {
				t.offsets[p] = info.Size()
			}
			return nil
		}
		t.readLocked(p)
		return nil
	})
}

// Update reads whatever was appended to path since the last read.
func (t *Tracker) Update(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.keep != nil && !t.keep(path) {
		return
	}
	t.readLocked(path)
}

func (t *Tracker) readLocked(p string) {
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	size := info.Size()
	off := t.offsets[p]
	if off > size { // truncated or replaced; dedup keys protect against double counting
		off = 0
	}
	if off == size {
		return
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return
	}
	cutoff := time.Now().Add(-keep)
	r := bufio.NewReaderSize(io.LimitReader(f, size-off), 64*1024)
	var consumed int64
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // EOF: a trailing partial line is left for the next read
		}
		consumed += int64(len(line))
		t.parseLocked(line, cutoff)
	}
	t.offsets[p] = off + consumed
}

var (
	markUsage     = []byte(`"usage"`)
	markAssistant = []byte(`"assistant"`)
)

type record struct {
	Type      string    `json:"type"`
	UUID      string    `json:"uuid"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (t *Tracker) parseLocked(line []byte, cutoff time.Time) {
	if !bytes.Contains(line, markUsage) || !bytes.Contains(line, markAssistant) {
		return
	}
	var r record
	if json.Unmarshal(line, &r) != nil || r.Type != "assistant" || r.Message.Usage == nil {
		return
	}
	if r.Message.Model == "<synthetic>" || r.Timestamp.Before(cutoff) {
		return
	}
	// One API response can be logged as several lines (one per content
	// block) carrying the same usage; count it once.
	key := r.RequestID
	if key == "" {
		key = r.Message.ID
	}
	if key == "" {
		key = r.UUID
	}
	u := r.Message.Usage
	e := entry{ts: r.Timestamp, in: u.Input, out: u.Output, cw: u.CacheWrite, cr: u.CacheRead}
	if old, ok := t.entries[key]; ok {
		e.ts = old.ts
		e.out = max(e.out, old.out)
	}
	t.entries[key] = e
	if r.Timestamp.After(t.last) {
		t.last = r.Timestamp
	}
}

// Stats sums entries at or after since and prunes expired ones.
func (t *Tracker) Stats(since time.Time) Stats {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-keep)
	s := Stats{LastActivity: t.last}
	for k, e := range t.entries {
		if e.ts.Before(cutoff) {
			delete(t.entries, k)
			continue
		}
		if e.ts.Before(since) {
			continue
		}
		s.Requests++
		s.Input += e.in
		s.Output += e.out
		s.CacheWrite += e.cw
		s.CacheRead += e.cr
		if i := int(e.ts.Sub(since) / SparkBucket); i >= 0 && i < SparkBuckets {
			s.Spark[i] += e.in + e.out + e.cw
		}
	}
	return s
}
