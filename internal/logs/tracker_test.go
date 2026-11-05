package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func line(req string, ts time.Time, in, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":%q,"message":{"id":"m","model":"claude-opus-5-5","usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":10,"cache_read_input_tokens":100}}}`+"\n",
		ts.UTC().Format(time.RFC3339Nano), req, in, out)
}

func TestTrackerIncrementalAndDedup(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "p")
	os.MkdirAll(proj, 0o755)
	file := filepath.Join(proj, "s.jsonl")
	now := time.Now()

	// Two lines of the same response (one per content block) + one other.
	os.WriteFile(file, []byte(
		`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"+
			line("r1", now.Add(-time.Hour), 5, 100)+
			line("r1", now.Add(-time.Hour), 5, 120)+
			line("old", now.Add(-7*time.Hour), 1, 1)), 0o644)

	tr := New(dir)
	tr.Scan()
	s := tr.Stats(now.Add(-5 * time.Hour))
	if s.Requests != 1 || s.Output != 120 || s.Input != 5 || s.CacheRead != 100 {
		t.Fatalf("after scan: %+v", s)
	}

	// Append one full line and one partial line.
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	l := line("r2", now, 7, 30)
	f.WriteString(l + l[:20])
	f.Close()
	tr.Update(file)
	s = tr.Stats(now.Add(-5 * time.Hour))
	if s.Requests != 2 || s.Output != 150 {
		t.Fatalf("after append: %+v", s)
	}

	// Complete the partial line: it is a duplicate of r2 and must not double count.
	f, _ = os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(l[20:])
	f.Close()
	tr.Update(file)
	s = tr.Stats(now.Add(-5 * time.Hour))
	if s.Requests != 2 || s.Output != 150 {
		t.Fatalf("after completing partial line: %+v", s)
	}
	if s.Spark[SparkBuckets-1] != 0 || s.Spark[24] == 0 {
		t.Fatalf("spark buckets: %v", s.Spark)
	}
}
