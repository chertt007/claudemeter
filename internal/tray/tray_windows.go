package tray

import (
	"runtime"
	"strings"
	"sync"

	"fyne.io/systray"
)

// Tray owns the notification-area icon. All methods are safe on a nil *Tray.
type Tray struct {
	events chan Event
	wake   chan struct{}

	mu   sync.Mutex
	want State

	// owned by the apply goroutine
	shown   State
	applied bool
	items   []menuItem
	stop    chan struct{}
}

type menuItem struct {
	item *systray.MenuItem
	text string
}

// Start shows the icon. It runs the Win32 message loop on its own OS thread.
func Start() *Tray {
	t := &Tray{events: make(chan Event, 8), wake: make(chan struct{}, 1)}
	go func() {
		runtime.LockOSThread()
		systray.Run(func() {
			systray.SetTooltip("ClaudeMeter")
			go t.loop()
		}, nil)
	}()
	return t
}

// Events delivers menu clicks.
func (t *Tray) Events() <-chan Event {
	if t == nil {
		return nil
	}
	return t.events
}

// Update records the state to show; it never blocks.
func (t *Tray) Update(s State) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.want = s
	t.mu.Unlock()
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// Stop removes the icon.
func (t *Tray) Stop() {
	if t != nil {
		systray.Quit()
	}
}

func (t *Tray) loop() {
	for range t.wake {
		t.mu.Lock()
		s := t.want
		t.mu.Unlock()
		t.apply(s)
	}
}

func (t *Tray) apply(s State) {
	first := !t.applied
	if first || iconKey(s.Gauges) != iconKey(t.shown.Gauges) {
		systray.SetIcon(Icon(s.Gauges))
	}
	if first || s.Tooltip != t.shown.Tooltip {
		systray.SetTooltip(clampTooltip(s.Tooltip))
	}
	if first || shape(s) != shape(t.shown) {
		t.rebuild(s)
	} else {
		t.retitle(s)
	}
	t.shown, t.applied = s, true
}

// shape describes the menu layout; when it changes the menu is rebuilt,
// otherwise only titles are updated.
func shape(s State) string {
	var b strings.Builder
	for _, a := range s.Accounts {
		b.WriteByte(byte('0' + min(len(a.Lines), 40)))
	}
	return b.String()
}

func (t *Tray) rebuild(s State) {
	if t.stop != nil {
		close(t.stop)
	}
	t.stop = make(chan struct{})
	systray.ResetMenu()
	t.items = t.items[:0]

	for i, a := range s.Accounts {
		if i > 0 {
			systray.AddSeparator()
		}
		title := systray.AddMenuItem(a.Title, "Select in the dashboard")
		t.items = append(t.items, menuItem{title, a.Title})
		t.on(title, Event{Kind: Select, Index: i})
		for _, l := range a.Lines {
			it := systray.AddMenuItem("      "+l, "")
			it.Disable()
			t.items = append(t.items, menuItem{it, l})
		}
	}
	if len(s.Accounts) > 0 {
		systray.AddSeparator()
	}
	t.on(systray.AddMenuItem("Refresh all", ""), Event{Kind: Refresh})
	t.on(systray.AddMenuItem("Quit ClaudeMeter", ""), Event{Kind: Quit})
	t.check(s)
}

func (t *Tray) retitle(s State) {
	k := 0
	for _, a := range s.Accounts {
		lines := append([]string{a.Title}, a.Lines...)
		for j, l := range lines {
			if k >= len(t.items) {
				return
			}
			if it := &t.items[k]; it.text != l {
				if j == 0 {
					it.item.SetTitle(l)
				} else {
					it.item.SetTitle("      " + l)
				}
				it.text = l
			}
			k++
		}
	}
	if s.Selected != t.shown.Selected {
		t.check(s)
	}
}

// check marks the account shown in the icon.
func (t *Tray) check(s State) {
	k := 0
	for i, a := range s.Accounts {
		if k < len(t.items) {
			if i == s.Selected {
				t.items[k].item.Check()
			} else {
				t.items[k].item.Uncheck()
			}
		}
		k += 1 + len(a.Lines)
	}
}

func (t *Tray) on(it *systray.MenuItem, ev Event) {
	stop := t.stop
	go func() {
		for {
			select {
			case <-it.ClickedCh:
				select {
				case t.events <- ev:
				default:
				}
			case <-stop:
				return
			}
		}
	}()
}
