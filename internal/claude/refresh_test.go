package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRefreshedKeepsOtherFields(t *testing.T) {
	dir := t.TempDir()
	src := `{"claudeAiOauth":{"accessToken":"old","refreshToken":"oldr","expiresAt":1,"scopes":["a","b"],"subscriptionType":"max"},"other":{"x":1}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRefreshed(dir, &tokenResponse{AccessToken: "new", RefreshToken: "newr", ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	cr, err := ReadCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cr.AccessToken != "new" || cr.RefreshToken != "newr" || cr.SubscriptionType != "max" {
		t.Fatalf("unexpected credentials: %+v", cr)
	}
	if cr.Expired(time.Now()) {
		t.Fatal("refreshed token should not be expired")
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	for _, want := range []string{`"scopes":["a","b"]`, `"other":{"x":1}`} {
		if !contains(string(b), want) {
			t.Errorf("lost %s in %s", want, b)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
