package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// A rifle stands next to each card. Its magazine is a gauge of what is left
// of the 5-hour session limit; it fires while Claude Code is writing in that
// profile and smokes for a moment afterwards.

const (
	gunPx     = 36 // bitmap width in pixels (= cells)
	gunW      = gunPx + 4
	gunGap    = 2
	shootFor  = 20 * time.Second
	smokeFor  = 8 * time.Second
	magRounds = 30
)

// account colours, assigned by position; the first one is the app accent.
var palette = []colorful.Color{
	mustHex("#D97757"), // orange
	mustHex("#60A5FA"), // blue
	mustHex("#A78BFA"), // violet
	mustHex("#2DD4BF"), // teal
	mustHex("#F472B6"), // pink
	mustHex("#FACC15"), // yellow
}

func acctColor(i int) colorful.Color { return palette[i%len(palette)] }

// The rifle, facing right. Each terminal row holds two pixel rows (half
// blocks). w = furniture (stock, grip, handguard), # = metal, m = magazine.
var gunBitmap = []string{
	`.....................#...........#..`,
	`..........###########wwwwwwwww###...`,
	`wwwwwwwwww############wwwwwwwww#####`,
	`wwwwwwwwww############wwwwwwww......`,
	`wwwwwww....www.#.mmmmm..............`,
	`wwww......www..##mmmmm..............`,
	`ww........www.....mmmmm.............`,
	`.........www......mmmmm.............`,
	`...................mmmmm............`,
	`....................mmmmm...........`,
	`.....................mmmm...........`,
	`....................................`,
}

const (
	muzzleRow = 1  // terminal row of the muzzle
	portX     = 18 // ejection port, where casings fly out
)

var (
	cMetal = mustHex("#8B95A5")
	cEmpty = mustHex("#2A3140")
	cFlash = []colorful.Color{mustHex("#FFF3B0"), mustHex("#FACC15"), mustHex("#FB923C")}
)

func (a *account) shooting(now time.Time) bool { return now.Sub(a.lastWrite) < shootFor }

// magFill returns how many magazine pixel rows are loaded and the rounds left.
func magFill(a *account) (rowsLoaded, rounds int, known bool) {
	if a.usage == nil {
		return 0, 0, false
	}
	left := math.Max(0, math.Min(100, 100-a.shownSession()))
	rounds = int(math.Round(left / 100 * magRounds))
	return int(math.Round(left / 100 * float64(magRows))), rounds, true
}

var magTop, magRows = func() (int, int) {
	top, bot := -1, -1
	for y, r := range gunBitmap {
		if strings.ContainsRune(r, 'm') {
			if top < 0 {
				top = y
			}
			bot = y
		}
	}
	return top, bot - top + 1
}()

// gun renders the sprite for an account: an effects row, the rifle and a
// rounds counter.
func (m *Model) gun(a *account, wood colorful.Color) string {
	firing := a.shooting(m.now) && m.cfg.AnimationsOn()
	loaded, rounds, known := magFill(a)
	empty := known && rounds == 0
	shot := firing && !empty && (m.frame/2)%2 == 0 // flash on this frame
	shift := 1
	if shot {
		shift = 0 // recoil
	}

	// Pixel colours; nil = transparent.
	h := len(gunBitmap)
	px := make([][]*colorful.Color, h)
	ammo := gradient(magRows)
	for y, r := range gunBitmap {
		px[y] = make([]*colorful.Color, gunW)
		for x, ch := range r {
			var c colorful.Color
			switch ch {
			case 'w':
				c = wood
			case '#':
				c = cMetal
			case 'm':
				c = cEmpty
				if fromBottom := magTop + magRows - 1 - y; fromBottom < loaded {
					c = ammo.colors[magRows-1-fromBottom] // full mag is green, last rounds red
				}
			default:
				continue
			}
			px[y][x+shift] = &c
		}
	}

	rows := make([]string, 0, h/2+2)
	rows = append(rows, m.gunFx(a, firing, empty))
	for r := 0; r < h/2; r++ {
		var b strings.Builder
		for x := 0; x < gunW; x++ {
			if r == muzzleRow && x > gunPx+shift-1 {
				if fx := muzzleFx(x-gunPx-shift, shot, m.frame); fx != "" {
					b.WriteString(fx)
					continue
				}
			}
			b.WriteString(halfBlock(px[2*r][x], px[2*r+1][x]))
		}
		rows = append(rows, b.String())
	}

	var label string
	switch {
	case !known:
		label = sFaint.Render("--/30")
	case empty:
		label = sErr.Bold(true).Render("EMPTY") + sFaint.Render(" · reload at reset")
	default:
		label = pctStyle(100-float64(rounds)/magRounds*100, "").Render(fmt.Sprintf("%2d/%d", rounds, magRounds)) +
			sFaint.Render(" rounds")
	}
	rows = append(rows, " "+label)
	for i, r := range rows {
		rows[i] = r + strings.Repeat(" ", max(0, gunW-lipgloss.Width(r)))
	}
	return strings.Join(rows, "\n")
}

// gunFx is the row above the rifle: flying casings while firing, smoke
// afterwards, "click" on an empty magazine.
func (m *Model) gunFx(a *account, firing, empty bool) string {
	line := []rune(strings.Repeat(" ", gunW))
	var style lipgloss.Style
	switch {
	case firing && empty:
		if (m.frame/6)%2 == 0 {
			copy(line[gunPx-6:], []rune("click!"))
		}
		style = sFaint
	case firing:
		// Two casings in flight, arcing up and back over the receiver.
		for _, off := range []int{0, 3} {
			t := (m.frame + off) % 6
			if x := portX + 1 + t; x < gunW {
				line[x] = []rune("▪▪▫▫··")[t]
			}
		}
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8B04B"))
	case m.cfg.AnimationsOn() && m.now.Sub(a.lastWrite) < shootFor+smokeFor && !a.lastWrite.IsZero():
		wisps := []string{"∿ ", " ∿", "~ ", " ~"}
		copy(line[gunPx:], []rune(wisps[int(m.now.UnixMilli()/400)%len(wisps)]))
		style = sFaint
	default:
		return string(line)
	}
	return style.Render(string(line))
}

// muzzleFx draws the flash in the cells past the muzzle (i = 0, 1, ...).
func muzzleFx(i int, shot bool, frame int) string {
	if !shot || i > 2 {
		return ""
	}
	shapes := [][]string{{"═", "✸", " "}, {"≡", "✷", "·"}}
	s := shapes[(frame/4)%2][i]
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cFlash[i].Hex())).Render(s)
}

// halfBlock draws two vertically stacked pixels in one cell.
func halfBlock(top, bot *colorful.Color) string {
	switch {
	case top == nil && bot == nil:
		return " "
	case bot == nil:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(top.Hex())).Render("▀")
	case top == nil:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(bot.Hex())).Render("▄")
	case *top == *bot:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(top.Hex())).Render("█")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(top.Hex())).Background(lipgloss.Color(bot.Hex())).Render("▀")
	}
}

// shownSession is the session % as currently displayed (eased).
func (a *account) shownSession() float64 {
	for _, b := range a.usage.Bars() {
		if b.Session {
			if v, ok := a.shown[b.Label]; ok {
				return v
			}
			return b.Percent
		}
	}
	return 0
}
