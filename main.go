// ClaudeMeter shows Claude Code subscription limits for several profiles at once.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chertt007/claudemeter/internal/claude"
	"github.com/chertt007/claudemeter/internal/config"
	"github.com/chertt007/claudemeter/internal/tray"
	"github.com/chertt007/claudemeter/internal/ui"
	"github.com/chertt007/claudemeter/internal/watch"
)

var version = "0.1.0-dev"

func main() {
	once := flag.Bool("once", false, "print a single snapshot and exit")
	width := flag.Int("width", 90, "snapshot width for --once")
	cfgPath := flag.String("config", "", "config file (default %APPDATA%/ClaudeMeter/config.json)")
	showVersion := flag.Bool("version", false, "print version and exit")
	noTray := flag.Bool("no-tray", false, "don't show the notification-area icon (Windows)")
	flag.Parse()

	if *showVersion {
		fmt.Println("claudemeter", version)
		return
	}
	claude.UserAgent = "claudemeter/" + version

	path := *cfgPath
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			fail(err)
		}
		path = p
	}
	cfg, err := config.LoadOrInit(path)
	if err != nil {
		fail(err)
	}

	if *once {
		fmt.Println(ui.New(cfg, config.LoadCache(path), nil).Snapshot(*width))
		return
	}

	w := watch.New()
	defer w.Close()
	m := ui.New(cfg, config.LoadCache(path), w)
	if cfg.TrayOn() && !*noTray {
		log.SetOutput(io.Discard) // the tray library logs to stderr, which would garble the TUI
		t := tray.Start()
		defer t.Stop()
		m.SetTray(t)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	// Closing the console window: quit cleanly so the tray icon is removed.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() { <-sig; p.Quit() }()
	if _, err := p.Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "claudemeter:", err)
	os.Exit(1)
}
