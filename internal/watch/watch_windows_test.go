package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchNestedAppend(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "c--some-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	w := New()
	defer w.Close()
	if err := w.Add(dir); err != nil {
		t.Fatal(err)
	}

	// Keep the file open like Claude Code does, and append to it.
	f, err := os.OpenFile(filepath.Join(proj, "s.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_ = os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644)
	if _, err := f.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}

	select {
	case batch := <-w.Events:
		for _, p := range batch {
			if filepath.Base(p) == "ignored.txt" {
				t.Errorf("irrelevant path delivered: %s", p)
			}
		}
		if len(batch) == 0 || filepath.Base(batch[0]) != "s.jsonl" {
			t.Fatalf("unexpected batch %v", batch)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event for appended transcript")
	}
}
