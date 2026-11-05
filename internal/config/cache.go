package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CachedUsage is the last successful usage response for a profile, kept so
// the dashboard has numbers to show right away even if the endpoint is
// temporarily refusing requests.
type CachedUsage struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Usage     json.RawMessage `json:"usage"`
}

type Cache struct {
	path    string
	Entries map[string]CachedUsage `json:"entries"` // key: lower(profile dir)
}

// LoadCache reads cache.json next to the config file. Errors yield an empty cache.
func LoadCache(configPath string) *Cache {
	c := &Cache{path: filepath.Join(filepath.Dir(configPath), "cache.json"), Entries: map[string]CachedUsage{}}
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, c)
		if c.Entries == nil {
			c.Entries = map[string]CachedUsage{}
		}
	}
	return c
}

func (c *Cache) Get(dir string) (CachedUsage, bool) {
	e, ok := c.Entries[strings.ToLower(dir)]
	return e, ok
}

func (c *Cache) Put(dir string, e CachedUsage) error {
	c.Entries[strings.ToLower(dir)] = e
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
