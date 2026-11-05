// ClaudeMeter shows Claude Code subscription limits for several profiles at once.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/chertt007/claudemeter/internal/claude"
	"github.com/chertt007/claudemeter/internal/config"
	"github.com/chertt007/claudemeter/internal/ui"
	"github.com/chertt007/claudemeter/internal/watch"
)

var version = "0.1.0-dev"

func main() {
	once := flag.Bool("once", false, "print a single snapshot and exit")
	width := flag.Int("width", 90, "snapshot width for --once")
	cfgPath := flag.String("config", "", "config file (default %APPDATA%/ClaudeMeter/config.json)")
	showVersion := flag.Bool("version", false, "print version and exit")
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
	if _, err := tea.NewProgram(ui.New(cfg, config.LoadCache(path), w), tea.WithAltScreen()).Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "claudemeter:", err)
	os.Exit(1)
}
