package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Same public OAuth client and endpoint Claude Code itself uses.
const (
	TokenURL = "https://platform.claude.com/v1/oauth/token"
	ClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// refreshMu serialises refreshes inside this process so two accounts sharing
// one profile directory don't burn the one-shot refresh token twice.
var refreshMu sync.Mutex

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// EnsureFresh returns usable credentials for the profile in dir. If the access
// token is expired it re-reads the file first (Claude Code may have refreshed
// it already), and otherwise refreshes it and writes the result back, keeping
// every other field of the file intact.
func EnsureFresh(ctx context.Context, dir string) (*Credentials, error) {
	cr, err := ReadCredentials(dir)
	if err != nil || !cr.Expired(time.Now()) {
		return cr, err
	}

	refreshMu.Lock()
	defer refreshMu.Unlock()

	// Someone else may have refreshed while we waited for the lock.
	if cr, err = ReadCredentials(dir); err != nil || !cr.Expired(time.Now()) {
		return cr, err
	}
	if cr.RefreshToken == "" {
		return nil, ErrTokenExpired
	}

	tr, err := requestRefresh(ctx, cr.RefreshToken)
	if err != nil {
		// The refresh token is single-use: if Claude Code rotated it between
		// our read and the POST, the file now holds a fresh pair.
		if again, rerr := ReadCredentials(dir); rerr == nil && !again.Expired(time.Now()) {
			return again, nil
		}
		return nil, err
	}
	if err := writeRefreshed(dir, tr); err != nil {
		return nil, fmt.Errorf("save refreshed credentials: %w", err)
	}
	return ReadCredentials(dir)
}

func requestRefresh(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     ClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// 400/401 mean the refresh token is dead: the user must log in again.
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrTokenExpired
		}
		return nil, &APIError{Status: resp.StatusCode}
	}
	var tr tokenResponse
	if err := json.Unmarshal(b, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" || tr.ExpiresIn <= 0 {
		return nil, fmt.Errorf("incomplete token response")
	}
	return &tr, nil
}

// writeRefreshed swaps the token fields in .credentials.json atomically,
// preserving unknown keys (scopes, subscription info, other sections).
func writeRefreshed(dir string, tr *tokenResponse) error {
	path := filepath.Join(dir, ".credentials.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(b, &root); err != nil {
		return err
	}
	var oauth map[string]json.RawMessage
	if err := json.Unmarshal(root["claudeAiOauth"], &oauth); err != nil {
		return err
	}
	set := func(k string, v any) {
		raw, _ := json.Marshal(v)
		oauth[k] = raw
	}
	set("accessToken", tr.AccessToken)
	set("refreshToken", tr.RefreshToken)
	set("expiresAt", time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second).UnixMilli())
	root["claudeAiOauth"], _ = json.Marshal(oauth)

	out, err := json.Marshal(root)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
