package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const usage = `grove - git-worktree switcher

usage:
  grove                        open this repo's grove window (focuses it if open)
  grove open <name>            open a workspace in the grove window
  grove open <name> -w         open a workspace in an external terminal (uses ` + "`terminal`" + `)
  grove new <intention> <br>   create a worktree (branch <br>) and open it
  grove remove <name>          remove a worktree (--force if dirty, --branch to delete its branch)
  grove init                   write a .grove.toml template in the current repo
  grove list                   print workspace names
  grove state <state>          report Claude Code state from a pane: working|waiting|idle|clear
  grove gui [name]             run the window in the foreground (for debugging)
  grove doctor                 check prerequisites
  grove version                print the version
  grove help                   show this help
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		if !fromTerminal() {
			runGUI("", false) // Finder/Dock launch, or `wails dev`
			return
		}
		fail(showInGUI(mustConfig(), "", false))
		return
	}

	switch args[0] {
	case "gui":
		name, setup := "", false
		for _, a := range args[1:] {
			if a == "--setup" {
				setup = true
			} else {
				name = a
			}
		}
		runGUI(name, setup)
	case "doctor":
		os.Exit(doctor())
	case "version", "--version", "-v":
		fmt.Println("grove", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "init":
		fail(initConfig())
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
			fail(errors.New("no [[repo]] configured to create into"))
		}
		base := ""
		if len(args) > 3 {
			base = args[3]
		}
		ws, err := createWorktree(cfg.Repo[0], args[1], args[2], base)
		fail(err)
		fmt.Printf("created %s at %s (branch %s)\n", ws.Name, ws.Dir, ws.Branch)
		fail(showInGUI(cfg, ws.Name, true))
	case "remove":
		removeCmd(args[1:])
	case "state":
		stateCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "grove: unknown command %q\n\n%s", args[0], usage)
		os.Exit(2)
	}
}

// fromTerminal reports whether grove was started from an interactive shell (so
// it should hand off to a detached window) rather than by Finder or `wails dev`.
func fromTerminal() bool {
	if os.Getenv("devserver") != "" { // set by `wails dev`
		return false
	}
	// A real tty, not merely a character device: Finder hands apps /dev/null.
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func fail(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "grove: %v\n", err)
		os.Exit(1)
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
	found := findWorkspace(cfg, name)
	if found == nil {
		fail(fmt.Errorf("no workspace named %q", name))
	}
	if newWindow {
		fail(openInTerminal(*found, cfg.Terminal))
		return
	}
	fail(showInGUI(cfg, found.Name, false))
}

// findWorkspace returns the resolved workspace with the given name, or nil.
func findWorkspace(cfg *Config, name string) *Workspace {
	for _, ws := range cfg.resolve() {
		if ws.Name == name {
			w := ws
			return &w
		}
	}
	return nil
}

// removeCmd implements `grove remove <name> [--force] [--branch]`: it removes the
// worktree (forcing past genuine uncommitted changes only with --force) and,
// with --branch, safely deletes its branch.
func removeCmd(args []string) {
	force, delBranch := false, false
	var name string
	for _, a := range args {
		switch a {
		case "--force", "-f":
			force = true
		case "--branch", "-b":
			delBranch = true
		default:
			name = a
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "grove remove: needs a workspace name")
		os.Exit(2)
	}
	cfg := mustConfig()
	found := findWorkspace(cfg, name)
	if found == nil {
		fail(fmt.Errorf("no workspace named %q", name))
	}
	if found.RepoPath == "" {
		fail(fmt.Errorf("%s is not a git worktree", name))
	}

	switch err := removeWorktree(found.RepoPath, found.Dir, force); {
	case errors.Is(err, errWorktreeDirty):
		fail(fmt.Errorf("%s has uncommitted changes; re-run with --force", name))
	default:
		fail(err)
	}
	// Stop its panes if the grove window has it open; absence is fine.
	_ = ipcCall(cfg.socketPath(), ipcReq{Op: "close", Name: found.Name})

	if delBranch {
		switch err := removeBranch(found.RepoPath, found.Branch); {
		case errors.Is(err, errBranchUnmerged):
			fmt.Printf("removed %s; branch %s kept (unmerged)\n", name, found.Branch)
			return
		case err != nil:
			fail(fmt.Errorf("removed %s but %v", name, err))
		default:
			fmt.Printf("removed %s and branch %s\n", name, found.Branch)
			return
		}
	}
	fmt.Printf("removed %s\n", name)
}

// stateCmd implements `grove state <state>`, run by Claude Code hooks inside a
// pane to mark its workspace in the sidebar. Outside a grove pane it does
// nothing, so the hooks are safe in any terminal.
func stateCmd(args []string) {
	pane, sock := os.Getenv("GROVE_PANE"), os.Getenv("GROVE_SOCK")
	if pane == "" || sock == "" {
		return
	}
	state := ""
	if len(args) > 0 {
		state = args[0]
	}
	switch state {
	case "working", "waiting", "idle":
	case "", "clear":
		state = ""
	default:
		fmt.Fprintf(os.Stderr, "grove state: unknown state %q (want working, waiting, idle or clear)\n", state)
		os.Exit(2)
	}
	if err := ipcCall(sock, ipcReq{Op: "state", Pane: pane, State: state}); err != nil && !errors.Is(err, errNotRunning) {
		fail(err)
	}
}

func mustConfig() *Config {
	cfg, err := loadConfig()
	fail(err)
	return cfg
}
