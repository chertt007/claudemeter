package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

var (
	cAccent = lipgloss.Color("#D97757")
	cText   = lipgloss.Color("#E5E7EB")
	cMuted  = lipgloss.Color("#9CA3AF")
	cFaint  = lipgloss.Color("#6B7280")
	cTrack  = lipgloss.Color("#374151")
	cBorder = lipgloss.Color("#4B5563")
	cGreen  = lipgloss.Color("#4ADE80")
	cYellow = lipgloss.Color("#FACC15")
	cRed    = lipgloss.Color("#F87171")

	sTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	sName  = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sMuted = lipgloss.NewStyle().Foreground(cMuted)
	sFaint = lipgloss.NewStyle().Foreground(cFaint)
	sTrack = lipgloss.NewStyle().Foreground(cTrack)
	sErr   = lipgloss.NewStyle().Foreground(cRed)
	sWarn  = lipgloss.NewStyle().Foreground(cYellow)
	sOK    = lipgloss.NewStyle().Foreground(cGreen)
	sKey   = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#111827")).Background(cAccent).Padding(0, 1)
	sCard  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(0, 1)
	sPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(0, 1)
)

// gradient stops for bar cells: green -> yellow -> red along the bar.
var stops = []colorful.Color{
	mustHex("#4ADE80"), mustHex("#A3E635"), mustHex("#FACC15"), mustHex("#FB923C"), mustHex("#F87171"),
}

func mustHex(s string) colorful.Color {
	c, _ := colorful.Hex(s)
	return c
}

type grad struct {
	colors []colorful.Color
	styles []lipgloss.Style
}

var gradCache = map[int]grad{}

func gradient(width int) grad {
	if g, ok := gradCache[width]; ok {
		return g
	}
	g := grad{colors: make([]colorful.Color, width), styles: make([]lipgloss.Style, width)}
	for i := range g.colors {
		t := (float64(i) + 0.5) / float64(max(width, 1)) * float64(len(stops)-1)
		k := min(int(t), len(stops)-2)
		c := stops[k].BlendLuv(stops[k+1], t-float64(k))
		g.colors[i] = c
		g.styles[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hex()))
	}
	gradCache[width] = g
	return g
}

var (
	white    = mustHex("#FFFFFF")
	partials = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
)

const shimmerHalf = 4.0 // half-width of the moving highlight, in cells

// bar renders a progress bar whose filled cells take their colour from their
// position, so a nearly full bar visibly "heats up" towards red. shimmer >= 0
// is the position (in cells) of a highlight sweeping across the filled part.
func bar(pct float64, width int, shimmer float64) string {
	pct = math.Max(0, math.Min(100, pct))
	g := gradient(width)
	cells := pct / 100 * float64(width)
	full := int(cells)
	part := int((cells - float64(full)) * 8)
	cell := func(i int, ch string) string {
		if shimmer >= 0 {
			if d := math.Abs(float64(i) - shimmer); d < shimmerHalf {
				k := (1 - d/shimmerHalf) * 0.65
				return lipgloss.NewStyle().Foreground(lipgloss.Color(g.colors[i].BlendRgb(white, k).Hex())).Render(ch)
			}
		}
		return g.styles[i].Render(ch)
	}
	var b strings.Builder
	for i := 0; i < full; i++ {
		b.WriteString(cell(i, "█"))
	}
	n := full
	if full < width && part > 0 {
		b.WriteString(cell(full, partials[part]))
		n++
	}
	if n < width {
		b.WriteString(sTrack.Render(strings.Repeat("░", width-n)))
	}
	return b.String()
}

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

// spark renders per-bucket values; buckets after `upto` (the future part of
// the window) are drawn as faint dots.
func spark(vals []int64, upto int) string {
	var top int64
	for _, v := range vals {
		top = max(top, v)
	}
	g := gradient(len(sparkLevels))
	var b strings.Builder
	for i, v := range vals {
		switch {
		case i > upto:
			b.WriteString(sTrack.Render("·"))
		case v == 0 || top == 0:
			b.WriteString(sTrack.Render("▁"))
		default:
			lv := int(float64(v) / float64(top) * float64(len(sparkLevels)-1))
			lv = max(0, min(lv, len(sparkLevels)-1))
			b.WriteString(g.styles[lv].Render(string(sparkLevels[lv])))
		}
	}
	return b.String()
}

// gradientText colours each rune along the accent gradient.
func gradientText(s string, from, to colorful.Color, bold bool) string {
	r := []rune(s)
	var b strings.Builder
	for i, ch := range r {
		t := 0.0
		if len(r) > 1 {
			t = float64(i) / float64(len(r)-1)
		}
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(from.BlendLuv(to, t).Hex())).Bold(bold)
		b.WriteString(st.Render(string(ch)))
	}
	return b.String()
}

// pulse returns a colour oscillating between c and a dim version of it.
func pulse(c colorful.Color, frame int) lipgloss.Style {
	k := (math.Sin(float64(frame)*0.35) + 1) / 2 * 0.7
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c.BlendRgb(mustHex("#1F2937"), k).Hex()))
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func pctStyle(pct float64, severity string) lipgloss.Style {
	switch {
	case pct >= 85 || severity == "critical" || severity == "exceeded":
		return sErr.Bold(true)
	case pct >= 60 || (severity != "" && severity != "normal"):
		return sWarn.Bold(true)
	default:
		return sOK.Bold(true)
	}
}

func fmtReset(t *time.Time, now time.Time) string {
	if t == nil {
		return ""
	}
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "resetting…"
	case d < time.Hour:
		return fmt.Sprintf("resets in %dm", int(math.Ceil(d.Minutes())))
	case d < 24*time.Hour:
		return fmt.Sprintf("resets in %dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return "resets " + t.Local().Format("Mon 15:04")
	}
}

func fmtAgo(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Local().Format("Jan 2")
	}
}

func fmtDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(math.Ceil(d.Seconds())))
	}
	return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func fmtTokens(n int64) string {
	f := float64(n)
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", f/1e3)
	case n < 1_000_000:
		return fmt.Sprintf("%.0fk", f/1e3)
	case n < 10_000_000:
		return fmt.Sprintf("%.2fM", f/1e6)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fM", f/1e6)
	default:
		return fmt.Sprintf("%.2fB", f/1e9)
	}
}

// spread places left and right on one line of the given width.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}
