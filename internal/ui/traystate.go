package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/chertt007/claudemeter/internal/claude"
	"github.com/chertt007/claudemeter/internal/tray"
)

type trayMsg tray.Event

// SetTray connects a notification-area icon to the dashboard.
func (m *Model) SetTray(t *tray.Tray) { m.tray = t }

func (m *Model) waitTray() tea.Cmd {
	ch := m.tray.Events()
	if ch == nil {
		return nil
	}
	return func() tea.Msg { return trayMsg(<-ch) }
}

func (m *Model) onTray(ev trayMsg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.waitTray()}
	switch ev.Kind {
	case tray.Select:
		if ev.Index >= 0 && ev.Index < len(m.accts) {
			m.sel = ev.Index
		}
	case tray.Refresh:
		for _, a := range m.accts {
			if !a.fetching {
				a.notBefore = time.Time{}
				cmds = append(cmds, m.fetch(a), m.scan(a))
			}
		}
		cmds = append(cmds, m.animate())
	case tray.Quit:
		return m, tea.Quit
	}
	return m, tea.Batch(cmds...)
}

// syncTray pushes the current numbers to the tray icon.
func (m *Model) syncTray() {
	if m.tray == nil {
		return
	}
	s := tray.State{Selected: m.sel}
	for _, a := range m.accts {
		g := tray.Gauge{Level: tray.Unknown}
		if a.usage != nil {
			if b, ok := sessionBar(a.usage); ok {
				g = tray.Gauge{Pct: b.Percent, Level: trayLevel(b.Percent, b.Severity)}
			}
		}
		s.Gauges = append(s.Gauges, g)
	}

	var tip []string
	short := make([]string, 0, len(m.accts))
	for _, a := range m.accts {
		long, brief := m.tipLine(a)
		tip = append(tip, long)
		short = append(short, brief)
		s.Accounts = append(s.Accounts, m.trayAccount(a))
	}
	s.Tooltip = strings.Join(tip, "\n")
	if len([]rune(s.Tooltip)) > 120 {
		s.Tooltip = strings.Join(short, "\n")
	}
	if s.Tooltip == "" {
		s.Tooltip = "ClaudeMeter: no subscriptions"
	}
	m.tray.Update(s)
}

func sessionBar(u *claude.Usage) (claude.Bar, bool) {
	for _, b := range u.Bars() {
		if b.Session {
			return b, true
		}
	}
	return claude.Bar{}, false
}

func trayLevel(pct float64, severity string) tray.Level {
	switch {
	case pct >= 85 || severity == "critical" || severity == "exceeded":
		return tray.Critical
	case pct >= 60 || (severity != "" && severity != "normal"):
		return tray.Warn
	default:
		return tray.OK
	}
}

// tipLine is one tooltip line, with and without reset times.
func (m *Model) tipLine(a *account) (long, brief string) {
	if a.usage == nil {
		return a.cfg.Name + ": no data", a.cfg.Name + ": no data"
	}
	var parts, short []string
	for _, b := range a.usage.Bars() {
		name := "week"
		switch {
		case b.Session:
			name = "5h"
		case b.Label != "Weekly":
			continue // keep the tooltip short; details are in the menu
		}
		p := fmt.Sprintf("%s %.0f%%", name, b.Percent)
		short = append(short, p)
		if b.Session && b.ResetsAt != nil && b.ResetsAt.After(m.now) {
			p += " → " + b.ResetsAt.Local().Format("15:04")
		}
		parts = append(parts, p)
	}
	return a.cfg.Name + ": " + strings.Join(parts, " · "), a.cfg.Name + ": " + strings.Join(short, " · ")
}

func (m *Model) trayAccount(a *account) tray.Account {
	title := a.cfg.Name
	if a.plan != "" {
		title += " — " + a.plan
	}
	var lines []string
	if a.usage != nil {
		for _, b := range a.usage.Bars() {
			l := fmt.Sprintf("%s:  %.0f%%", b.Label, b.Percent)
			if r := fmtReset(b.ResetsAt, m.now); r != "" {
				l += "  ·  " + r
			}
			lines = append(lines, l)
		}
	} else {
		lines = append(lines, "no data yet")
	}
	st := a.stats
	lines = append(lines, fmt.Sprintf("5h tokens:  %s in  ·  %s out  ·  %d req",
		fmtTokens(st.Input+st.CacheWrite), fmtTokens(st.Output), st.Requests))
	if a.err != nil {
		lines = append(lines, ansi.Strip(errText(a, m.now)))
	}
	return tray.Account{Title: title, Lines: lines}
}
