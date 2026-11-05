package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UsageURL is the (undocumented) endpoint Claude Code's /usage screen uses.
const UsageURL = "https://api.anthropic.com/api/oauth/usage"

var (
	UserAgent  = "claudemeter"
	httpClient = &http.Client{Timeout: 15 * time.Second}
)

type Window struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

type Limit struct {
	Kind     string     `json:"kind"`
	Group    string     `json:"group"`
	Percent  float64    `json:"percent"`
	Severity string     `json:"severity"`
	ResetsAt *time.Time `json:"resets_at"`
	IsActive bool       `json:"is_active"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Surface json.RawMessage `json:"surface"`
	} `json:"scope"`
}

type ExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
	Currency     *string  `json:"currency"`
}

type Usage struct {
	FiveHour       *Window     `json:"five_hour"`
	SevenDay       *Window     `json:"seven_day"`
	SevenDayOpus   *Window     `json:"seven_day_opus"`
	SevenDaySonnet *Window     `json:"seven_day_sonnet"`
	Limits         []Limit     `json:"limits"`
	ExtraUsage     *ExtraUsage `json:"extra_usage"`
}

// Bar is one progress bar to display.
type Bar struct {
	Label    string
	Percent  float64
	Severity string
	ResetsAt *time.Time
	Session  bool
}

// Bars turns the response into display rows. The generic limits[] array is
// preferred so new limit kinds show up without code changes; the legacy
// fixed fields are a fallback.
func (u *Usage) Bars() []Bar {
	var out []Bar
	if len(u.Limits) > 0 {
		for _, l := range u.Limits {
			out = append(out, Bar{
				Label:    l.label(),
				Percent:  l.Percent,
				Severity: l.Severity,
				ResetsAt: l.ResetsAt,
				Session:  l.Kind == "session",
			})
		}
	} else {
		add := func(label string, w *Window, session bool) {
			if w != nil {
				out = append(out, Bar{Label: label, Percent: w.Utilization, ResetsAt: w.ResetsAt, Session: session})
			}
		}
		add("Session · 5h", u.FiveHour, true)
		add("Weekly", u.SevenDay, false)
		add("Weekly · Opus", u.SevenDayOpus, false)
		add("Weekly · Sonnet", u.SevenDaySonnet, false)
	}
	if e := u.ExtraUsage; e != nil && e.IsEnabled && e.Utilization != nil {
		out = append(out, Bar{Label: "Extra usage", Percent: *e.Utilization})
	}
	return out
}

// SessionResetsAt returns when the current 5-hour window resets, if known.
func (u *Usage) SessionResetsAt() *time.Time {
	for _, b := range u.Bars() {
		if b.Session {
			return b.ResetsAt
		}
	}
	return nil
}

func (l Limit) label() string {
	switch l.Kind {
	case "session":
		return "Session · 5h"
	case "weekly_all":
		return "Weekly"
	case "weekly_scoped":
		if name := l.scopeName(); name != "" {
			return "Weekly · " + name
		}
		return "Weekly · scoped"
	}
	s := strings.ReplaceAll(l.Kind, "_", " ")
	if name := l.scopeName(); name != "" {
		s += " · " + name
	}
	return title(s)
}

func (l Limit) scopeName() string {
	if l.Scope == nil {
		return ""
	}
	if l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
		return l.Scope.Model.DisplayName
	}
	if len(l.Scope.Surface) > 0 {
		var obj struct {
			DisplayName string `json:"display_name"`
		}
		if json.Unmarshal(l.Scope.Surface, &obj) == nil && obj.DisplayName != "" {
			return obj.DisplayName
		}
		var s string
		if json.Unmarshal(l.Scope.Surface, &s) == nil {
			return s
		}
	}
	return ""
}

// APIError is a non-200 response.
type APIError struct {
	Status     int
	RetryAfter time.Duration
	Message    string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "unauthorized (token rejected)"
	case http.StatusTooManyRequests:
		return "rate limited by usage endpoint"
	}
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

// FetchUsage queries the usage endpoint. It does not consume any quota.
func FetchUsage(ctx context.Context, token string) (*Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		e := &APIError{Status: resp.StatusCode}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.RetryAfter = time.Duration(s) * time.Second
		}
		var eb struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &eb) == nil {
			e.Message = eb.Error.Message
		}
		return nil, e
	}
	var u Usage
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("parse usage: %w", err)
	}
	return &u, nil
}
