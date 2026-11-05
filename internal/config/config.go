// Package config stores the list of tracked Claude Code profiles.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Account is one Claude Code profile: a config directory (what CLAUDE_CONFIG_DIR
// points to) holding its own .credentials.json and session logs.
type Account struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

type Config struct {
	Accounts []Account `json:"accounts"`
	// PollSeconds is the background refresh interval when nothing happens locally.
	PollSeconds int `json:"poll_seconds,omitempty"`
	// MinRefreshSeconds is the minimum gap between two usage requests for the
	// same account while Claude Code is actively writing logs.
	MinRefreshSeconds int `json:"min_refresh_seconds,omitempty"`
	// Animations can be set to false to render static frames only.
	Animations *bool `json:"animations,omitempty"`

	path string
}

const (
	defaultPoll       = 180
	defaultMinRefresh = 30
)

func (c *Config) Poll() time.Duration {
	if c.PollSeconds <= 0 {
		return defaultPoll * time.Second
	}
	return time.Duration(c.PollSeconds) * time.Second
}

func (c *Config) MinRefresh() time.Duration {
	if c.MinRefreshSeconds <= 0 {
		return defaultMinRefresh * time.Second
	}
	return time.Duration(c.MinRefreshSeconds) * time.Second
}

func (c *Config) AnimationsOn() bool { return c.Animations == nil || *c.Animations }

func (c *Config) Path() string { return c.path }

// DefaultPath is %APPDATA%\ClaudeMeter\config.json on Windows.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ClaudeMeter", "config.json"), nil
}

// LoadOrInit reads the config at path. If the file does not exist yet it is
// created with auto-discovered profiles.
func LoadOrInit(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := &Config{Accounts: Discover(nil), path: path}
		return c, c.Save()
	}
	if err != nil {
		return nil, err
	}
	c := &Config{path: path}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range c.Accounts {
		c.Accounts[i].Dir = Normalize(c.Accounts[i].Dir)
	}
	return c, nil
}

// Save writes the config atomically.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Has reports whether dir is already tracked.
func (c *Config) Has(dir string) bool {
	dir = Normalize(dir)
	for _, a := range c.Accounts {
		if strings.EqualFold(a.Dir, dir) {
			return true
		}
	}
	return false
}

// Normalize expands ~ and makes the path absolute and clean.
func Normalize(p string) string {
	p = strings.TrimSpace(strings.Trim(strings.TrimSpace(p), `"'`))
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	p = os.ExpandEnv(p)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// HasCredentials reports whether dir contains a Claude Code login.
func HasCredentials(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".credentials.json"))
	return err == nil
}

// Discover finds Claude Code profile directories (~/.claude, ~/.claude-*, and
// $CLAUDE_CONFIG_DIR) that contain credentials and are not in skip.
func Discover(skip *Config) []Account {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		entries, _ := os.ReadDir(home)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), ".claude") {
				dirs = append(dirs, filepath.Join(home, e.Name()))
			}
		}
	}
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		dirs = append(dirs, env)
	}

	seen := map[string]bool{}
	var out []Account
	for _, d := range dirs {
		d = Normalize(d)
		key := strings.ToLower(d)
		if seen[key] || !HasCredentials(d) || (skip != nil && skip.Has(d)) {
			continue
		}
		seen[key] = true
		out = append(out, Account{Name: NameFromDir(d), Dir: d})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name == "default" && out[j].Name != "default"
	})
	return out
}

// NameFromDir derives a display name: ~/.claude -> "default",
// ~/.claude-personal -> "personal".
func NameFromDir(dir string) string {
	base := filepath.Base(dir)
	switch {
	case strings.EqualFold(base, ".claude"):
		return "default"
	case strings.HasPrefix(strings.ToLower(base), ".claude-"):
		return base[len(".claude-"):]
	default:
		return strings.TrimPrefix(base, ".")
	}
}
