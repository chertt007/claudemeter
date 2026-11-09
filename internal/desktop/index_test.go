package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionOf(t *testing.T) {
	root := filepath.Join("C:", "u", ".claude", "projects")
	cases := map[string]string{
		filepath.Join(root, "proj", "abc-123.jsonl"):                       "abc-123",
		filepath.Join(root, "proj", "abc-123", "subagents", "agent.jsonl"): "abc-123",
		filepath.Join(root, "top.jsonl"):                                   "",
		filepath.Join("C:", "elsewhere", "proj", "x.jsonl"):                "",
	}
	for p, want := range cases {
		if got := SessionOf(root, p); got != want {
			t.Errorf("SessionOf(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestIndexOrg(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "acct-1", "ORG-A")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"cliSessionId":"Sess-1","model":"x"}`)
	if err := os.WriteFile(filepath.Join(dir, "local_1.json"), meta, 0o644); err != nil {
		t.Fatal(err)
	}
	ix := &Index{root: root, parsed: map[string]bool{}, orgs: map[string]string{}}
	if org, ok := ix.Org("sess-1"); !ok || org != "org-a" {
		t.Fatalf("Org = %q, %v; want org-a, true", org, ok)
	}
	if _, ok := ix.Org("unknown"); ok {
		t.Fatal("unknown session reported as desktop")
	}
}
