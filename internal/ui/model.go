// Package ui is the Bubble Tea terminal interface.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/chertt007/claudemeter/internal/claude"
	"github.com/chertt007/claudemeter/internal/config"
	"github.com/chertt007/claudemeter/internal/logs"
	"github.com/chertt007/claudemeter/internal/tray"
	"github.com/chertt007/claudemeter/internal/watch"
)

// debounce is how long to wait after a transcript write before asking the
// server, so one model reply produces one request.
const debounce = 3 * time.Second

type account struct {
	id      int
	cfg     config.Account
	ident   claude.Identity
	plan    string
	tracker *logs.Tracker

	usage       *claude.Usage
	stats       logs.Stats
	fetchedAt   time.Time // last success
	attemptAt   time.Time // last attempt
	fetching    bool
	err         error
	wantFetchAt time.Time     // scheduled fetch (zero = none)
	notBefore   time.Time     // backoff after errors
	limited     int           // consecutive 429 responses
	gap         time.Duration // adaptive min gap between activity-driven refreshes
	okStreak    int           // successes since the gap was last changed

	// animation state
	shown     map[string]float64 // displayed bar values, eased towards targets
	lastWrite time.Time          // last transcript write seen (drives shimmer)
	delta     float64            // session % increase from the last refresh
	deltaAt   time.Time
}

type mode int

const (
	modeNormal mode = iota
	modeAdd
	modeDelete
)

type Model struct {
	cfg     *config.Config
	cache   *config.Cache
	watcher *watch.Watcher
	accts   []*account
	nextID  int
	sel     int
	mode    mode
	inputs  [2]textinput.Model
	focus   int
	formErr string
	notice  string
	width   int
	height  int
	now     time.Time

	frame     int
	animating bool

	tray *tray.Tray
}

type (
	tickMsg  time.Time
	frameMsg time.Time
	fsMsg    []string
	statsMsg struct {
		id    int
		stats logs.Stats
	}
	usageMsg struct {
		id    int
		usage *claude.Usage
		plan  string
		err   error
		at    time.Time
	}
)

func New(cfg *config.Config, cache *config.Cache, w *watch.Watcher) *Model {
	m := &Model{cfg: cfg, cache: cache, watcher: w, now: time.Now(), width: 80}
	for _, a := range cfg.Accounts {
		m.addAccount(a)
	}
	return m
}

func (m *Model) addAccount(c config.Account) *account {
	m.nextID++
	a := &account{
		id:      m.nextID,
		cfg:     c,
		ident:   claude.ReadIdentity(c.Dir),
		tracker: logs.New(c.Dir),
		shown:   map[string]float64{},
	}
	if cr, err := claude.ReadCredentials(c.Dir); err == nil {
		a.plan = cr.Plan()
	}
	if m.cache != nil {
		if e, ok := m.cache.Get(c.Dir); ok {
			var u claude.Usage
			if json.Unmarshal(e.Usage, &u) == nil {
				a.usage, a.fetchedAt = &u, e.FetchedAt
			}
		}
	}
	if m.watcher != nil {
		if err := m.watcher.Add(c.Dir); err != nil {
			m.notice = "live updates off for " + c.Name + ": " + err.Error()
		}
	}
	m.accts = append(m.accts, a)
	return a
}

func (m *Model) Init() tea.Cmd {
	m.syncTray()
	cmds := []tea.Cmd{tea.SetWindowTitle("ClaudeMeter"), tick(), m.waitFS(), m.waitTray()}
	for _, a := range m.accts {
		cmds = append(cmds, m.fetch(a), m.scan(a))
	}
	cmds = append(cmds, m.animate())
	return tea.Batch(cmds...)
}

// Animation runs at ~16 fps only while something moves: bars easing to new
// values, a spinner during a request, or the shimmer while Claude Code is
// writing. Otherwise the UI repaints once a second.
const (
	frameEvery = 60 * time.Millisecond
	liveFor    = 6 * time.Second
	deltaFor   = 10 * time.Second
)

func frame() tea.Cmd {
	return tea.Tick(frameEvery, func(t time.Time) tea.Msg { return frameMsg(t) })
}

func (m *Model) animate() tea.Cmd {
	if m.animating || !m.cfg.AnimationsOn() || !m.needsFrames() {
		return nil
	}
	m.animating = true
	return frame()
}

func (a *account) live(now time.Time) bool { return now.Sub(a.lastWrite) < liveFor }

func (m *Model) needsFrames() bool {
	for _, a := range m.accts {
		if a.fetching || a.live(m.now) || a.easing() || (m.cfg.GunOn() && m.now.Sub(a.lastWrite) < shootFor+smokeFor) {
			return true
		}
	}
	return false
}

func (a *account) easing() bool {
	if a.usage == nil {
		return false
	}
	for _, b := range a.usage.Bars() {
		if v, ok := a.shown[b.Label]; !ok || math.Abs(v-b.Percent) > 0.05 {
			return true
		}
	}
	return false
}

// ease moves displayed values towards their targets (or snaps if animations
// are off).
func (m *Model) ease() {
	for _, a := range m.accts {
		if a.usage == nil {
			continue
		}
		for _, b := range a.usage.Bars() {
			v := a.shown[b.Label]
			d := b.Percent - v
			if !m.cfg.AnimationsOn() || math.Abs(d) < 0.05 {
				v = b.Percent
			} else {
				v += d * 0.16
			}
			a.shown[b.Label] = v
		}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) waitFS() tea.Cmd {
	if m.watcher == nil {
		return nil
	}
	ch := m.watcher.Events
	return func() tea.Msg { return fsMsg(<-ch) }
}

func (m *Model) byID(id int) *account {
	for _, a := range m.accts {
		if a.id == id {
			return a
		}
	}
	return nil
}

// accountFor maps a changed path to the profile containing it.
func (m *Model) accountFor(p string) *account {
	lp := strings.ToLower(p)
	for _, a := range m.accts {
		if strings.HasPrefix(lp, strings.ToLower(a.cfg.Dir)+string(filepath.Separator)) {
			return a
		}
	}
	return nil
}

// sessionStart is the start of the current 5h window, used for token tallies.
func (a *account) sessionStart(now time.Time) time.Time {
	if a.usage != nil {
		if r := a.usage.SessionResetsAt(); r != nil {
			if r.After(now) {
				return r.Add(-5 * time.Hour)
			}
			return *r
		}
	}
	return now.Add(-5 * time.Hour)
}

func (m *Model) fetch(a *account) tea.Cmd {
	a.fetching = true
	a.wantFetchAt = time.Time{}
	a.attemptAt = m.now
	id, dir := a.id, a.cfg.Dir
	return func() tea.Msg {
		msg := usageMsg{id: id}
		defer func() { msg.at = time.Now() }()
		cr, err := claude.ReadCredentials(dir)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.plan = cr.Plan()
		if cr.Expired(time.Now()) {
			msg.err = claude.ErrTokenExpired
			return msg
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		msg.usage, msg.err = claude.FetchUsage(ctx, cr.AccessToken)
		msg.at = time.Now()
		return msg
	}
}

func (m *Model) scan(a *account) tea.Cmd {
	t, id, since := a.tracker, a.id, a.sessionStart(m.now)
	return func() tea.Msg {
		t.Scan()
		return statsMsg{id: id, stats: t.Stats(since)}
	}
}

func (m *Model) updateFiles(a *account, paths []string) tea.Cmd {
	t, id, since := a.tracker, a.id, a.sessionStart(m.now)
	return func() tea.Msg {
		for _, p := range paths {
			t.Update(p)
		}
		return statsMsg{id: id, stats: t.Stats(since)}
	}
}

func (m *Model) restats(a *account) tea.Cmd {
	t, id, since := a.tracker, a.id, a.sessionStart(m.now)
	return func() tea.Msg { return statsMsg{id: id, stats: t.Stats(since)} }
}

const maxGap = 5 * time.Minute

// gap is the current minimum interval between refreshes for an account. It
// doubles on every 429 and slowly shrinks back after a run of successes, so
// the app settles just under whatever rate the endpoint tolerates.
func (m *Model) gap(a *account) time.Duration {
	return max(a.gap, m.cfg.MinRefresh())
}

// schedule asks for a usage refresh soon, without exceeding MinRefresh.
func (m *Model) schedule(a *account, delay time.Duration) {
	if !a.wantFetchAt.IsZero() {
		return // already scheduled; don't let a busy session postpone it forever
	}
	at := m.now.Add(delay)
	if gap := a.attemptAt.Add(m.gap(a)); gap.After(at) {
		at = gap
	}
	a.wantFetchAt = at
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	if _, anim := msg.(frameMsg); !anim {
		m.syncTray()
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case trayMsg:
		return m.onTray(msg)

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case frameMsg:
		m.now = time.Time(msg)
		m.frame++
		m.ease()
		if m.cfg.AnimationsOn() && m.needsFrames() {
			return m, frame()
		}
		m.animating = false
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if !m.animating {
			m.ease()
		}
		var cmds []tea.Cmd
		for _, a := range m.accts {
			if a.fetching || m.now.Before(a.notBefore) {
				continue
			}
			due := !a.wantFetchAt.IsZero() && !m.now.Before(a.wantFetchAt)
			stale := m.now.Sub(a.attemptAt) >= m.cfg.Poll()
			// A window just reset: fetch fresh numbers instead of showing 99%.
			if a.usage != nil && a.err == nil {
				for _, b := range a.usage.Bars() {
					if b.ResetsAt != nil && b.ResetsAt.Before(m.now) && a.attemptAt.Before(*b.ResetsAt) {
						due = true
					}
				}
			}
			if due || stale {
				cmds = append(cmds, m.fetch(a))
			}
		}
		cmds = append(cmds, tick(), m.animate())
		return m, tea.Batch(cmds...)

	case fsMsg:
		m.now = time.Now()
		cmds := []tea.Cmd{m.waitFS()}
		byAcct := map[*account][]string{}
		for _, p := range msg {
			if p == watch.Rescan {
				for _, a := range m.accts {
					cmds = append(cmds, m.scan(a))
				}
				continue
			}
			if a := m.accountFor(p); a != nil {
				byAcct[a] = append(byAcct[a], p)
			}
		}
		for a, paths := range byAcct {
			var logs []string
			for _, p := range paths {
				if strings.EqualFold(filepath.Base(p), ".credentials.json") {
					// Claude Code refreshed or replaced the login.
					a.ident = claude.ReadIdentity(a.cfg.Dir)
					if a.err != nil || a.usage == nil {
						a.notBefore = time.Time{}
						m.schedule(a, time.Second)
					}
				} else {
					logs = append(logs, p)
				}
			}
			if len(logs) > 0 {
				a.lastWrite = m.now
				cmds = append(cmds, m.updateFiles(a, logs))
				m.schedule(a, debounce)
			}
		}
		cmds = append(cmds, m.animate())
		return m, tea.Batch(cmds...)

	case statsMsg:
		if a := m.byID(msg.id); a != nil {
			a.stats = msg.stats
		}
		return m, nil

	case usageMsg:
		a := m.byID(msg.id)
		if a == nil {
			return m, nil
		}
		a.fetching = false
		if msg.plan != "" {
			a.plan = msg.plan
		}
		a.err = msg.err
		if msg.err == nil {
			if a.usage != nil {
				if d := sessionPct(msg.usage) - sessionPct(a.usage); d >= 0.5 {
					a.delta, a.deltaAt = d, msg.at
				}
			}
			a.usage = msg.usage
			a.fetchedAt = msg.at
			a.notBefore = time.Time{}
			a.limited = 0
			if a.okStreak++; a.okStreak >= 4 && a.gap > m.cfg.MinRefresh() {
				a.gap, a.okStreak = a.gap/2, 0
			}
			if m.cache != nil {
				if b, err := json.Marshal(msg.usage); err == nil {
					_ = m.cache.Put(a.cfg.Dir, config.CachedUsage{FetchedAt: msg.at, Usage: b})
				}
			}
			return m, tea.Batch(m.restats(a), m.animate())
		}
		var apiErr *claude.APIError
		switch {
		case errors.As(msg.err, &apiErr) && apiErr.Status == 429:
			// Exponential backoff: 30s, 1m, 2m, ... capped at 10m, unless the
			// server says exactly how long to wait.
			a.limited++
			a.gap, a.okStreak = min(m.gap(a)*2, maxGap), 0
			wait := min(30*time.Second<<min(a.limited-1, 5), 10*time.Minute)
			if apiErr.RetryAfter > 0 {
				wait = apiErr.RetryAfter
			}
			a.notBefore = msg.at.Add(wait)
		case errors.As(msg.err, &apiErr) && apiErr.RetryAfter > 0:
			a.notBefore = msg.at.Add(apiErr.RetryAfter)
		case errors.Is(msg.err, claude.ErrTokenExpired), errors.Is(msg.err, claude.ErrNotLoggedIn):
			// Nothing to do until the credentials file changes (watched).
		default:
			a.notBefore = msg.at.Add(30 * time.Second)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeAdd:
			return m.updateAdd(msg)
		case modeDelete:
			return m.updateDelete(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func sessionPct(u *claude.Usage) float64 {
	for _, b := range u.Bars() {
		if b.Session {
			return b.Percent
		}
	}
	return 0
}

func (m *Model) updateNormal(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch k.String() {
	case "q", "ctrl+c", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.sel > 0 {
			m.sel--
		}
	case "down", "j", "tab":
		if m.sel < len(m.accts)-1 {
			m.sel++
		}
	case "r":
		if a := m.selected(); a != nil && !a.fetching {
			a.notBefore = time.Time{}
			return m, tea.Batch(m.fetch(a), m.scan(a), m.animate())
		}
	case "R":
		var cmds []tea.Cmd
		for _, a := range m.accts {
			if !a.fetching {
				a.notBefore = time.Time{}
				cmds = append(cmds, m.fetch(a), m.scan(a))
			}
		}
		cmds = append(cmds, m.animate())
		return m, tea.Batch(cmds...)
	case "g":
		on := !m.cfg.GunOn()
		m.cfg.Gun = &on
		if err := m.cfg.Save(); err != nil {
			m.notice = "Could not save config: " + err.Error()
		}
		return m, m.animate()
	case "a", "+", "insert":
		return m, m.openAdd()
	case "d", "-", "delete":
		if m.selected() != nil {
			m.mode = modeDelete
		}
	}
	return m, nil
}

func (m *Model) selected() *account {
	if m.sel >= 0 && m.sel < len(m.accts) {
		return m.accts[m.sel]
	}
	return nil
}

func (m *Model) openAdd() tea.Cmd {
	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "work"
	name.CharLimit = 32
	dir := textinput.New()
	dir.Prompt = ""
	dir.Placeholder = `C:\Users\you\.claude-work`
	dir.CharLimit = 260
	if found := config.Discover(m.cfg); len(found) > 0 {
		name.SetValue(found[0].Name)
		dir.SetValue(found[0].Dir)
	}
	m.inputs = [2]textinput.Model{name, dir}
	m.focus = 0
	m.formErr = ""
	m.mode = modeAdd
	return m.inputs[0].Focus()
}

func (m *Model) setFocus(i int) tea.Cmd {
	m.focus = (i + len(m.inputs)) % len(m.inputs)
	for j := range m.inputs {
		m.inputs[j].Blur()
	}
	return m.inputs[m.focus].Focus()
}

func (m *Model) updateAdd(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "ctrl+c":
		m.mode = modeNormal
		return m, nil
	case "tab", "down":
		return m, m.setFocus(m.focus + 1)
	case "shift+tab", "up":
		return m, m.setFocus(m.focus - 1)
	case "enter":
		if m.focus == 0 {
			return m, m.setFocus(1)
		}
		return m, m.submitAdd()
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(k)
	m.formErr = ""
	return m, cmd
}

func (m *Model) submitAdd() tea.Cmd {
	dir := config.Normalize(m.inputs[1].Value())
	name := strings.TrimSpace(m.inputs[0].Value())
	if dir == "" {
		m.formErr = "Enter the profile folder (the one CLAUDE_CONFIG_DIR points to)."
		return nil
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		m.formErr = "Folder not found: " + dir
		return nil
	}
	if m.cfg.Has(dir) {
		m.formErr = "This profile is already on the list."
		return nil
	}
	if name == "" {
		name = config.NameFromDir(dir)
	}
	acc := config.Account{Name: name, Dir: dir}
	m.cfg.Accounts = append(m.cfg.Accounts, acc)
	if err := m.cfg.Save(); err != nil {
		m.cfg.Accounts = m.cfg.Accounts[:len(m.cfg.Accounts)-1]
		m.formErr = "Could not save config: " + err.Error()
		return nil
	}
	a := m.addAccount(acc)
	m.sel = len(m.accts) - 1
	m.mode = modeNormal
	if !config.HasCredentials(dir) {
		m.notice = "Added. Log in there with:  $env:CLAUDE_CONFIG_DIR='" + dir + "'; claude  — the card updates by itself."
	}
	return tea.Batch(m.fetch(a), m.scan(a), m.animate())
}

func (m *Model) updateDelete(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y", "enter":
		a := m.selected()
		if a == nil {
			break
		}
		for i, c := range m.cfg.Accounts {
			if strings.EqualFold(c.Dir, a.cfg.Dir) {
				m.cfg.Accounts = append(m.cfg.Accounts[:i], m.cfg.Accounts[i+1:]...)
				break
			}
		}
		if err := m.cfg.Save(); err != nil {
			m.notice = "Could not save config: " + err.Error()
		}
		if m.watcher != nil {
			m.watcher.Remove(a.cfg.Dir)
		}
		m.accts = append(m.accts[:m.sel], m.accts[m.sel+1:]...)
		if m.sel >= len(m.accts) {
			m.sel = max(0, len(m.accts)-1)
		}
	}
	m.mode = modeNormal
	return m, nil
}
