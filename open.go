package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// All worktrees live as windows inside one shared tmux session. tmux does the
// heavy lifting, so grove is terminal-agnostic: the default flow attaches in the
// current terminal, and the only terminal-specific action (opening a brand-new
// window) is driven by a configurable command template.
const (
	sessionName   = "grove"
	switcherWidth = 30 // fixed width (cols) of the left switcher pane
)

func tmux(args ...string) error {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tmuxOut(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// sessionExists reports whether the shared grove session is alive.
func sessionExists() bool {
	return exec.Command("tmux", "has-session", "-t", "="+sessionName).Run() == nil
}

// windowExists reports whether a tab (tmux window) for this workspace exists.
func windowExists(name string) bool {
	if !sessionExists() {
		return false
	}
	out, err := tmuxOut("list-windows", "-t", sessionName, "-F", "#{window_name}")
	if err != nil {
		return false
	}
	for _, w := range strings.Split(out, "\n") {
		if w == name {
			return true
		}
	}
	return false
}

// activeWindow returns the name of the session's currently selected window.
func activeWindow() string {
	if !sessionExists() {
		return ""
	}
	out, _ := tmuxOut("display-message", "-t", sessionName, "-p", "#{window_name}")
	return out
}

// clientAttached reports whether a terminal is attached to the session.
func clientAttached() bool {
	if !sessionExists() {
		return false
	}
	out, err := tmuxOut("list-clients", "-t", sessionName)
	return err == nil && out != ""
}

// splitCapture splits a pane and returns the new pane's id. dir is "-h" or "-v";
// size is a tmux -l size (e.g. "82%") or "" for an even split; cmd is the pane's
// program (run directly to avoid a send-keys race) or "" for a plain shell.
func splitCapture(dir, target, cwd, size, cmd string) (string, error) {
	args := []string{"split-window", dir, "-t", target, "-c", cwd, "-P", "-F", "#{pane_id}"}
	if size != "" {
		args = append(args, "-l", size)
	}
	if strings.TrimSpace(cmd) != "" {
		args = append(args, cmd)
	}
	return tmuxOut(args...)
}

// buildLayout fills a freshly-created window (whose only pane id is firstPane)
// with the fixed arrangement:
//
//	┌───────┬────────┬─────────┐
//	│ grove │ pane0  │ pane1   │   left: grove switcher
//	│ list  │ (big)  ├─────────┤   middle: first config pane (claude)
//	│       │        │ pane2   │   right column: remaining config panes
//	└───────┴────────┴─────────┘
func buildLayout(ws Workspace, firstPane string) error {
	panes := ws.Panes
	if len(panes) == 0 {
		panes = []string{""}
	}

	// firstPane (left) already runs the embedded switcher (its window command).
	// Middle "big" pane takes most of the width; the switcher keeps the slim remainder.
	big, err := splitCapture("-h", firstPane, ws.Dir, "82%", panes[0])
	if err != nil {
		return err
	}

	// Remaining config panes stack in a right-hand column.
	prev := big
	for i := 1; i < len(panes); i++ {
		var np string
		if i == 1 {
			np, err = splitCapture("-h", big, ws.Dir, "40%", panes[i]) // carve right column off the big pane
		} else {
			np, err = splitCapture("-v", prev, ws.Dir, "50%", panes[i]) // stack below previous
		}
		if err != nil {
			return err
		}
		prev = np
	}

	// Pin the switcher pane to a slim fixed width. tmux rescales panes
	// proportionally when the window resizes (e.g. on attach), so also install a
	// window-resized hook to re-pin it.
	pin := fmt.Sprintf("resize-pane -t %s -x %d", firstPane, switcherWidth)
	_ = tmux(strings.Fields(pin)...)
	_ = tmux("set-hook", "-w", "-t", sessionName+":"+ws.Name, "window-resized", pin)
	// Stamp the repo name on the window so set-titles-string can show it.
	repo := ws.RepoName
	if strings.TrimSpace(repo) == "" {
		repo = ws.Name
	}
	_ = tmux("set-option", "-w", "-t", sessionName+":"+ws.Name, "@grove_repo", repo)
	// Land the cursor on the big (work) pane.
	_ = tmux("select-pane", "-t", big)
	return nil
}

// ensureWindow makes sure a tab for this workspace exists, creating the session
// and/or window (with its pane layout) as needed.
func configureSession() {
	// Drive the terminal's title from the active worktree's repo, so the grove
	// window is findable among other terminals. @grove_repo is set per window in
	// buildLayout; the format resolves against whichever window is active.
	_ = tmux("set-option", "-t", sessionName, "set-titles", "on")
	_ = tmux("set-option", "-t", sessionName, "set-titles-string", "grove - #{@grove_repo}")
	// Show a tab strip even with one window.
	_ = tmux("set-option", "-t", sessionName, "status", "on")
	// Click to select panes/tabs and scroll (escape full-screen TUIs).
	_ = tmux("set-option", "-t", sessionName, "mouse", "on")
	// Make the selected pane obvious: bright active border vs dim inactive,
	// plus heavy box lines on the active pane for a shape cue too.
	_ = tmux("set-option", "-t", sessionName, "pane-border-style", "fg=colour238")
	_ = tmux("set-option", "-t", sessionName, "pane-active-border-style", "fg=colour40,bold")
	_ = tmux("set-option", "-t", sessionName, "pane-border-lines", "heavy")
}

// groveBin returns the path to the grove executable, run as the left switcher
// pane in every window.
func groveBin() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "grove"
}

func ensureWindow(ws Workspace) error {
	if !sessionExists() {
		// First window also creates the session; capture its pane id.
		// The window's command is the embedded grove switcher (left pane).
		id, err := tmuxOut("new-session", "-d", "-s", sessionName, "-n", ws.Name,
			"-c", ws.Dir, "-P", "-F", "#{pane_id}", groveBin())
		if err != nil {
			return fmt.Errorf("new-session: %w", err)
		}
		configureSession()
		return buildLayout(ws, id)
	}
	configureSession()
	if windowExists(ws.Name) {
		return nil
	}
	id, err := tmuxOut("new-window", "-t", sessionName, "-n", ws.Name,
		"-c", ws.Dir, "-P", "-F", "#{pane_id}", groveBin())
	if err != nil {
		return fmt.Errorf("new-window: %w", err)
	}
	return buildLayout(ws, id)
}

// expandTerminal substitutes the {cmd} placeholder in a terminal template with
// the command grove wants the new window to run.
func expandTerminal(tmpl, cmd string) string {
	if strings.Contains(tmpl, "{cmd}") {
		return strings.ReplaceAll(tmpl, "{cmd}", cmd)
	}
	// No placeholder: append the command (covers "ghostty -e", "kitty", etc.).
	return strings.TrimSpace(tmpl) + " " + cmd
}

// launchTerminal opens a new terminal window running cmd, using the configured
// template (e.g. "ghostty -e {cmd}", "wezterm start -- {cmd}"). Empty template
// is an error so the optional -w feature degrades with a clear message.
func launchTerminal(terminal, cmd string) error {
	if strings.TrimSpace(terminal) == "" {
		return fmt.Errorf("no `terminal` set in config; required to open a new window (-w)")
	}
	c := exec.Command("sh", "-c", expandTerminal(terminal, cmd))
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	return c.Start()
}

// prepare ensures the tab exists and is the session's selected window.
func prepare(ws Workspace) error {
	if err := ensureWindow(ws); err != nil {
		return err
	}
	return tmux("select-window", "-t", sessionName+":"+ws.Name)
}

// attachHere takes over the CURRENT terminal: it replaces this process with
// `tmux attach` (same tab), or switches the client if already inside tmux.
func attachHere(ws Workspace) error {
	if err := prepare(ws); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		// Already inside tmux: just switch the attached client's window.
		return tmux("switch-client", "-t", sessionName+":"+ws.Name)
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	// Replace grove with tmux attach so it owns the tty cleanly.
	return syscall.Exec(bin, []string{"tmux", "attach", "-t", sessionName}, os.Environ())
}

// openWindow opens the workspace in a NEW terminal window (the -w path), using
// the configured terminal template.
func openWindow(ws Workspace, terminal string) error {
	if err := prepare(ws); err != nil {
		return err
	}
	return launchTerminal(terminal, "tmux attach -t "+sessionName)
}

// shellPaneIndex returns the window pane index of the first plain-shell pane.
// Layout: switcher = pane 0, panes[0] = pane 1, panes[i] = pane i+1. The shell pane
// is the first empty entry in panes; falls back to the big pane (1).
func shellPaneIndex(panes []string) int {
	for i, c := range panes {
		if strings.TrimSpace(c) == "" {
			return i + 1
		}
	}
	return 1
}

// runSetup runs the repo's setup command in the new worktree's shell pane.
func runSetup(ws Workspace, setup string) error {
	if strings.TrimSpace(setup) == "" {
		return nil
	}
	target := fmt.Sprintf("%s:%s.%d", sessionName, ws.Name, shellPaneIndex(ws.Panes))
	return tmux("send-keys", "-t", target, setup, "Enter")
}

// createAndOpen creates a new worktree, builds + selects its window, then runs
// the repo's setup command in the shell pane.
func createAndOpen(r Repo, intention, branch, base string) (Workspace, error) {
	ws, err := createWorktree(r, intention, branch, base)
	if err != nil {
		return ws, err
	}
	if err := prepare(ws); err != nil {
		return ws, err
	}
	if err := runSetup(ws, r.Setup); err != nil {
		return ws, err
	}
	return ws, nil
}

// closeWindow kills the tmux window (tab) for a workspace. The worktree on disk
// is untouched; only the window and its panes are destroyed.
func closeWindow(name string) error {
	if !windowExists(name) {
		return fmt.Errorf("%s is not open", name)
	}
	return tmux("kill-window", "-t", sessionName+":"+name)
}

// statusOf returns a short live status for a workspace tab.
func statusOf(name string) string {
	if !windowExists(name) {
		return ""
	}
	if clientAttached() && activeWindow() == name {
		return "active"
	}
	return "open"
}
