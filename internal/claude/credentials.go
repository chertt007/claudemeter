// Package claude reads Claude Code profile data and queries the usage endpoint.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotLoggedIn  = errors.New("not logged in")
	ErrTokenExpired = errors.New("token expired")
)

// Credentials is the subset of <dir>/.credentials.json we need. The file is
// read here; refreshing (see EnsureFresh) writes the rotated pair back.
type Credentials struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"` // unix ms
	SubscriptionType string `json:"subscriptionType"`
	RateLimitTier    string `json:"rateLimitTier"`
}

func ReadCredentials(dir string) (*Credentials, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		OAuth *Credentials `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if f.OAuth == nil || f.OAuth.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return f.OAuth, nil
}

func (c *Credentials) Expired(now time.Time) bool {
	return c.ExpiresAt > 0 && now.UnixMilli() >= c.ExpiresAt-30_000
}

// Plan returns a human label such as "Pro" or "Team · Max 5x".
func (c *Credentials) Plan() string {
	sub := title(c.SubscriptionType)
	if i := strings.Index(c.RateLimitTier, "max_"); i >= 0 {
		m := "Max " + c.RateLimitTier[i+len("max_"):]
		if sub == "" || strings.EqualFold(sub, "max") {
			return m
		}
		return sub + " · " + m
	}
	return sub
}

// Identity is who the profile is logged in as.
type Identity struct {
	Email   string
	Org     string
	OrgUUID string
}

// ReadIdentity reads oauthAccount from the profile's .claude.json. For the
// default ~/.claude profile that file lives next to the directory (~/.claude.json).
func ReadIdentity(dir string) Identity {
	paths := []string{filepath.Join(dir, ".claude.json")}
	if strings.EqualFold(filepath.Base(dir), ".claude") {
		paths = append(paths, filepath.Join(filepath.Dir(dir), ".claude.json"))
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f struct {
			OAuthAccount *struct {
				EmailAddress     string `json:"emailAddress"`
				OrganizationName string `json:"organizationName"`
				OrganizationUUID string `json:"organizationUuid"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(b, &f) == nil && f.OAuthAccount != nil {
			return Identity{
				Email:   f.OAuthAccount.EmailAddress,
				Org:     f.OAuthAccount.OrganizationName,
				OrgUUID: strings.ToLower(f.OAuthAccount.OrganizationUUID),
			}
		}
	}
	return Identity{}
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
