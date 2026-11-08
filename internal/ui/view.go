package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"

	"github.com/chertt007/claudemeter/internal/claude"
	"github.com/chertt007/claudemeter/internal/logs"
)

var (
	hAccent = mustHex("#D97757")
	hWarm   = mustHex("#F5B971")
	hGreen  = mustHex("#4ADE80")
)

const labelW = 16

// layout returns the card width and whether the rifles fit beside the cards.
func (m *Model) layout() (int, bool) {
	if m.cfg.GunOn() && len(m.accts) > 0 {
		if w := min(m.width-1-gunGap-gunW, 100); w >= 72 {
			return w, true
		}
	}
	return max(48, min(m.width-1, 100)), false
}

func (m *Model) View() string {
	w, guns := m.layout()
	var parts []string
	parts = append(parts, m.header(w))

	if len(m.accts) == 0 && m.mode != modeAdd {
		parts = append(parts, sCard.Width(w-2).Render(
			sMuted.Render("No subscriptions yet.")+"\n"+
				sFaint.Render("Press ")+sKey.Render("a")+sFaint.Render(" to add a Claude Code profile folder (e.g. ~/.claude-work).")))
	}
	for i, a := range m.accts {
		c := m.card(a, i == m.sel, acctColor(i), w)
		if guns {
			c = lipgloss.JoinHorizontal(lipgloss.Bottom, c, strings.Repeat(" ", gunGap), m.gun(a, acctColor(i)))
		}
		parts = append(parts, c)
	}
	switch m.mode {
	case modeAdd:
		parts = append(parts, m.addForm(w))
	case modeDelete:
		if a := m.selected(); a != nil {
			parts = append(parts, sPanel.Width(w-2).Render(
				sWarn.Render("Remove ")+sName.Render(a.cfg.Name)+sWarn.Render(" from ClaudeMeter? ")+
					sFaint.Render("(the profile itself is not touched)  ")+sKey.Render("y")+sFaint.Render("/")+sKey.Render("n")))
		}
	}
	if m.notice != "" {
		parts = append(parts, " "+sWarn.Render(m.notice))
	}
	parts = append(parts, m.help())
	return strings.Join(parts, "\n")
}

func (m *Model) header(w int) string {
	logo := sTitle.Render("▍") + gradientText("ClaudeMeter", hAccent, hWarm, true) + sFaint.Render("  subscription limits")

	live := false
	for _, a := range m.accts {
		live = live || a.live(m.now)
	}
	var status string
	if live {
		status = pulse(hGreen, m.frame).Render("●") + sOK.Bold(true).Render(" LIVE")
	} else {
		status = sFaint.Render("◌ idle")
	}
	right := status + sFaint.Render("  ") + sMuted.Render(m.now.Format("15:04:05"))
	top := " " + spread(logo, right, w-2)
	rule := " " + gradientText(strings.Repeat("━", w-2), hAccent, mustHex("#1F2937"), false)
	return top + "\n" + rule
}

func (m *Model) card(a *account, selected bool, col colorful.Color, w int) string {
	iw := w - 4 // border + padding
	var rows []string

	// Title row.
	lc := lipgloss.Color(col.Hex())
	accent := lipgloss.NewStyle().Bold(true).Foreground(lc)
	marker, name := sFaint.Render("◇ "), accent.Render(a.cfg.Name)
	if selected {
		marker = accent.Render("◆ ")
	}
	who := a.ident.Email
	if a.ident.Org != "" && !strings.Contains(strings.ToLower(a.ident.Org), strings.ToLower(a.ident.Email)) {
		who += " · " + a.ident.Org
	}
	badge := ""
	if a.plan != "" {
		badge = sBadge.Background(lc).Render(a.plan)
	}
	maxWho := iw - lipgloss.Width(marker+name) - lipgloss.Width(badge) - 4
	left := marker + name
	if who != "" && maxWho > 8 {
		left += "  " + sMuted.Render(truncate(who, maxWho))
	}
	rows = append(rows, spread(left, badge, iw), "")

	// Bars.
	if a.usage != nil {
		rows = append(rows, m.bars(a, iw)...)
	} else {
		var msg string
		switch {
		case a.err != nil:
			msg = sFaint.Render("no data")
		case a.fetching:
			msg = sTitle.Render(spinner[m.frame%len(spinner)]) + sMuted.Render(" fetching limits…")
		default:
			msg = sFaint.Render("waiting…")
		}
		rows = append(rows, msg)
	}

	// Token activity from local transcripts.
	rows = append(rows, "", m.activity(a, iw), m.status(a, iw))

	st := sCard.BorderForeground(lipgloss.Color(col.BlendLuv(mustHex("#1F2937"), 0.55).Hex()))
	if selected {
		st = sCard.BorderForeground(lc)
	}
	return st.Width(w - 2).Render(strings.Join(rows, "\n"))
}

func (m *Model) bars(a *account, iw int) []string {
	showReset := iw >= 64
	resetW := 0
	if showReset {
		resetW = 18
	}
	bw := iw - labelW - 1 - 5 - 1 - resetW
	var rows []string
	for _, b := range a.usage.Bars() {
		v, ok := a.shown[b.Label]
		if !ok {
			v = 0
		}
		shimmer := -1.0
		if b.Session && a.live(m.now) && m.cfg.AnimationsOn() {
			filled := v / 100 * float64(bw)
			span := filled + 2*shimmerHalf
			shimmer = float64(m.frame%int(max(span, 1)*1.6))/1.6 - shimmerHalf
		}
		label := sMuted.Render(fmt.Sprintf("%-*s", labelW, truncate(b.Label, labelW)))
		pct := pctStyle(b.Percent, b.Severity).Render(fmt.Sprintf("%4.0f%%", v))
		row := label + bar(v, bw, shimmer) + " " + pct
		if showReset {
			var right string
			if b.Session && a.delta > 0 && m.now.Sub(a.deltaAt) < deltaFor {
				right = sTitle.Bold(true).Render(fmt.Sprintf("▲ +%.0f%%", a.delta))
			} else {
				right = sFaint.Render(fmtReset(b.ResetsAt, m.now))
			}
			row += " " + lipgloss.PlaceHorizontal(resetW, lipgloss.Right, right)
		}
		rows = append(rows, row)
	}
	return rows
}

func (m *Model) activity(a *account, iw int) string {
	s := a.stats
	since := a.sessionStart(m.now)
	upto := int(m.now.Sub(since) / logs.SparkBucket)
	label := sMuted.Render(fmt.Sprintf("%-*s", labelW, "5h tokens"))
	line := label + spark(s.Spark[:], upto)
	sep := sFaint.Render(" · ")
	info := sFaint.Render("  ") +
		sKey.Render(fmtTokens(s.Input+s.CacheWrite)) + sFaint.Render(" in") + sep +
		sKey.Render(fmtTokens(s.Output)) + sFaint.Render(" out") + sep +
		sMuted.Render(fmtTokens(s.CacheRead)) + sFaint.Render(" cached") + sep +
		sMuted.Render(fmt.Sprint(s.Requests)) + sFaint.Render(" req")
	if lipgloss.Width(line+info) <= iw {
		line += info
	}
	return line
}

func (m *Model) status(a *account, iw int) string {
	var left string
	switch {
	case a.live(m.now):
		left = pulse(hGreen, m.frame).Render("●") + sOK.Render(" Claude is working")
	case !a.stats.LastActivity.IsZero():
		left = sFaint.Render("○ last activity " + fmtAgo(a.stats.LastActivity, m.now))
	default:
		left = sFaint.Render("○ no activity in this window")
	}

	var right string
	switch {
	case a.fetching && a.usage != nil:
		right = sTitle.Render(spinner[m.frame%len(spinner)]) + sFaint.Render(" updating")
	case a.err != nil:
		right = errText(a, m.now)
		if !a.fetchedAt.IsZero() {
			right = sFaint.Render("data "+fmtAgo(a.fetchedAt, m.now)+" · ") + right
		}
	case !a.fetchedAt.IsZero():
		right = sFaint.Render("⟳ " + fmtAgo(a.fetchedAt, m.now))
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+1 > iw {
		return left + "\n" + right
	}
	return spread(left, right, iw)
}

func errText(a *account, now time.Time) string {
	var apiErr *claude.APIError
	switch {
	case errors.Is(a.err, claude.ErrTokenExpired):
		return sWarn.Render("⚠ token expired · open claude in this profile")
	case errors.Is(a.err, claude.ErrNotLoggedIn):
		return sWarn.Render("⚠ not logged in")
	case errors.As(a.err, &apiErr) && apiErr.Status == 401:
		return sWarn.Render("⚠ token rejected · open claude to re-login")
	case errors.As(a.err, &apiErr) && apiErr.Status == 429:
		if d := a.notBefore.Sub(now); d > 0 {
			return sWarn.Render("⚠ rate limited · retry in " + fmtDur(d))
		}
		return sWarn.Render("⚠ rate limited · retrying")
	default:
		return sErr.Render("⚠ " + truncate(a.err.Error(), 48))
	}
}

func (m *Model) addForm(w int) string {
	field := func(i int, label string) string {
		lb := sMuted.Render(fmt.Sprintf("%-8s", label))
		pfx := sFaint.Render("  ")
		if m.focus == i {
			lb = sTitle.Render(fmt.Sprintf("%-8s", label))
			pfx = sTitle.Render("▸ ")
		}
		m.inputs[i].Width = w - 18
		return pfx + lb + m.inputs[i].View()
	}
	rows := []string{
		gradientText("Add subscription", hAccent, hWarm, true),
		"",
		field(0, "Name"),
		field(1, "Folder"),
		"",
		sFaint.Render("The folder is a Claude Code profile (what CLAUDE_CONFIG_DIR points to)."),
		sFaint.Render("To create one:  $env:CLAUDE_CONFIG_DIR=\"$HOME\\.claude-work\"; claude  → /login"),
	}
	if m.formErr != "" {
		rows = append(rows, "", sErr.Render("⚠ "+m.formErr))
	}
	rows = append(rows, "", keyHelp("tab", "next field")+keyHelp("enter", "save")+keyHelp("esc", "cancel"))
	return sPanel.Width(w - 2).Render(strings.Join(rows, "\n"))
}

func keyHelp(k, desc string) string {
	return sKey.Render(k) + " " + sFaint.Render(desc) + "   "
}

func (m *Model) help() string {
	if m.mode != modeNormal {
		return ""
	}
	return " " + keyHelp("↑↓", "select") + keyHelp("a", "add") + keyHelp("d", "remove") +
		keyHelp("r", "refresh") + keyHelp("R", "all") + keyHelp("g", "gun") + keyHelp("q", "quit")
}

// Snapshot loads everything synchronously and renders one static frame
// (used by --once).
func (m *Model) Snapshot(width int) string {
	m.width = width
	m.now = time.Now()
	for _, a := range m.accts {
		m.Update(m.fetch(a)())
		m.Update(m.scan(a)())
		m.Update(m.restats(a)())
		a.lastWrite = time.Time{}
		if a.usage != nil {
			for _, b := range a.usage.Bars() {
				a.shown[b.Label] = b.Percent
			}
		}
	}
	m.frame = 0
	return m.View()
}
