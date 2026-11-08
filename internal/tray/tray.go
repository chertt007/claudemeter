// Package tray shows a notification-area icon with one session gauge per
// account, a short tooltip and a menu with details for all accounts.
package tray

// Level picks the icon colour.
type Level int

const (
	Unknown Level = iota
	OK
	Warn
	Critical
)

// Account is one menu section: a clickable title and read-only detail lines.
type Account struct {
	Title string
	Lines []string
}

// Gauge is one vertical bar in the icon: an account's session usage.
type Gauge struct {
	Pct   float64 // 0..100; ignored when Level is Unknown
	Level Level
}

// State is everything the tray displays. The UI builds it; the tray only
// renders what changed.
type State struct {
	Gauges   []Gauge
	Tooltip  string
	Accounts []Account
	Selected int
}

type EventKind int

const (
	Select EventKind = iota
	Refresh
	Quit
)

// Event is a menu click.
type Event struct {
	Kind  EventKind
	Index int // account index for Select
}

// maxTip is the tooltip limit of Shell_NotifyIcon (128 UTF-16 units with NUL).
const maxTip = 127

// clampTooltip keeps s within the tooltip limit.
func clampTooltip(s string) string {
	n := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if n+w > maxTip-1 {
			return s[:i] + "…"
		}
		n += w
	}
	return s
}
