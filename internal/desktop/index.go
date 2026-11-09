// Package desktop maps Claude desktop app "Code" sessions to the organization
// they run under.
//
// The desktop app runs Claude Code with the default profile directory, so its
// transcripts land in ~/.claude/projects whichever account the app is logged
// into. The app keeps per-session metadata under
// <config>/Claude/claude-code-sessions/<account>/<organization>/local_*.json,
// whose cliSessionId names the transcript; that tells us which subscription
// the tokens belong to.
package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// rescanEvery limits directory listings when an unknown session is looked up.
const rescanEvery = 5 * time.Second

type Index struct {
	root string

	mu      sync.Mutex
	parsed  map[string]bool   // metadata files already read
	orgs    map[string]string // cliSessionId -> organization uuid
	scanned time.Time
}

// New indexes the desktop app's sessions; it is empty if the app isn't installed.
func New() *Index {
	ix := &Index{parsed: map[string]bool{}, orgs: map[string]string{}}
	if base, err := os.UserConfigDir(); err == nil {
		ix.root = filepath.Join(base, "Claude", "claude-code-sessions")
	}
	return ix
}

// Org returns the organization of a Claude Code session id, if the desktop
// app started it.
func (ix *Index) Org(session string) (string, bool) {
	if ix == nil || ix.root == "" || session == "" {
		return "", false
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if org, ok := ix.orgs[strings.ToLower(session)]; ok {
		return org, true
	}
	if time.Since(ix.scanned) < rescanEvery {
		return "", false
	}
	ix.scanLocked()
	org, ok := ix.orgs[strings.ToLower(session)]
	return org, ok
}

func (ix *Index) scanLocked() {
	ix.scanned = time.Now()
	files, _ := filepath.Glob(filepath.Join(ix.root, "*", "*", "local_*.json"))
	for _, f := range files {
		if ix.parsed[f] {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var meta struct {
			CLISessionID string `json:"cliSessionId"`
		}
		if json.Unmarshal(b, &meta) != nil || meta.CLISessionID == "" {
			continue // not written yet; retry on a later scan
		}
		ix.parsed[f] = true
		org := filepath.Base(filepath.Dir(f))
		ix.orgs[strings.ToLower(meta.CLISessionID)] = strings.ToLower(org)
	}
}

// SessionOf extracts the session id from a transcript path under projects:
// projects/<project>/<session>.jsonl or projects/<project>/<session>/subagents/….
func SessionOf(projects, path string) string {
	rel, err := filepath.Rel(projects, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSuffix(parts[1], ".jsonl")
}
