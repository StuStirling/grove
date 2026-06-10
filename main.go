package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `grove - tmux git-worktree switcher

usage:
  grove                        launch the TUI switcher (attaches in this tab)
  grove open <name>            attach a workspace in the current tab
  grove open <name> -w         open the workspace in a new terminal window
  grove new <intention> <br>   create a worktree (branch <br>) and open it
  grove init                   write a .grove.toml template in the current repo
  grove list                   print workspace names
  grove doctor                 check prerequisites (tmux, git)
  grove version                print the version
  grove help                   show this help
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		runTUI()
		return
	}

	switch args[0] {
	case "doctor":
		os.Exit(doctor())
	case "version", "--version", "-v":
		fmt.Println("grove", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "init":
		if err := initConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "grove: %v\n", err)
			os.Exit(1)
		}
	case "list":
		for _, ws := range mustConfig().resolve() {
			fmt.Println(ws.Name)
		}
	case "open":
		openCmd(args[1:])
	case "new":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "grove new: needs <intention> <branch> [base]")
			os.Exit(2)
		}
		cfg := mustConfig()
		if len(cfg.Repo) == 0 {
			fmt.Fprintln(os.Stderr, "grove: no [[repo]] configured to create into")
			os.Exit(1)
		}
		base := ""
		if len(args) > 3 {
			base = args[3]
		}
		ws, err := createAndOpen(cfg.Repo[0], args[1], args[2], base)
		if err != nil {
			fmt.Fprintf(os.Stderr, "grove: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("created %s at %s (branch %s)\n", ws.Name, ws.Dir, ws.Branch)
	default:
		fmt.Fprintf(os.Stderr, "grove: unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

func openCmd(args []string) {
	newWindow := false
	var name string
	for _, a := range args {
		switch a {
		case "--window", "-w":
			newWindow = true
		default:
			name = a
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "grove open: needs a workspace name")
		os.Exit(2)
	}
	cfg := mustConfig()
	var found *Workspace
	for _, ws := range cfg.resolve() {
		if ws.Name == name {
			w := ws
			found = &w
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "grove: no workspace named %q\n", name)
		os.Exit(1)
	}

	var err error
	if newWindow {
		err = openWindow(*found, cfg.Terminal)
	} else {
		err = attachHere(*found)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "grove: %v\n", err)
		os.Exit(1)
	}
}

func runTUI() {
	embedded := os.Getenv("TMUX") != ""
	p := tea.NewProgram(newModel(mustConfig(), embedded))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "grove: %v\n", err)
		os.Exit(1)
	}
	// Launcher mode: after the TUI restores the terminal, attach in this tab.
	if fm, ok := final.(model); ok && fm.chosen != nil {
		if err := attachHere(*fm.chosen); err != nil {
			fmt.Fprintf(os.Stderr, "grove: %v\n", err)
			os.Exit(1)
		}
	}
}

func mustConfig() *Config {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "grove: %v\n", err)
		os.Exit(1)
	}
	return cfg
}
