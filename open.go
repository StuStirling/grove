package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// showInGUI brings the repo's grove window forward, optionally opening a
// workspace in it (setup: a fresh worktree whose setup command should run). It
// launches the GUI when none is running.
func showInGUI(cfg *Config, name string, setup bool) error {
	req := ipcReq{Op: "focus", Name: name, Setup: setup}
	if name != "" {
		req.Op = "open"
	}
	err := ipcCall(cfg.socketPath(), req)
	if !errors.Is(err, errNotRunning) {
		return err
	}
	return spawnGUI(name, setup)
}

// spawnGUI starts `grove gui` detached from this terminal, like `code .`: the
// shell gets its prompt back and the window outlives the tab. It inherits this
// process's env and cwd, so it resolves the same config and PATH.
func spawnGUI(name string, setup bool) error {
	exe, err := groveExe()
	if err != nil {
		return err
	}
	args := []string{"gui"}
	if setup {
		args = append(args, "--setup")
	}
	if name != "" {
		args = append(args, name)
	}
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

// groveExe is this grove binary's real path. Symlinks are resolved so a binary
// linked from inside grove.app still runs as the bundle (dock icon, app name).
func groveExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// expandTerminal fills a terminal template: {cmd} becomes cmd and {dir} the
// worktree path. With neither placeholder, cmd is appended ("kitty" etc.).
func expandTerminal(tmpl, cmd, dir string) string {
	if !strings.Contains(tmpl, "{cmd}") && !strings.Contains(tmpl, "{dir}") {
		return strings.TrimSpace(tmpl) + " " + cmd
	}
	return strings.NewReplacer("{cmd}", cmd, "{dir}", shellQuote(dir)).Replace(tmpl)
}

// shellQuote single-quotes s for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// openInTerminal opens a worktree in a new external terminal window using the
// configured template. {cmd} is a login shell started in the worktree.
func openInTerminal(ws Workspace, terminal string) error {
	if strings.TrimSpace(terminal) == "" {
		return fmt.Errorf("no `terminal` set in config; needed to open an external terminal")
	}
	inner := "cd " + shellQuote(ws.Dir) + " && exec " + shellQuote(userShell()) + " -l"
	c := exec.Command("sh", "-c", expandTerminal(terminal, "sh -c "+shellQuote(inner), ws.Dir))
	c.Dir = ws.Dir
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }() // reap
	return nil
}

// fixPath gives a GUI launched outside a terminal (Finder, Dock) the user's login
// PATH, so pane commands like `claude` and `lazygit` resolve as they do in a
// terminal. A launch from a terminal already inherits it.
func fixPath() {
	if os.Getenv("TERM") != "" {
		return
	}
	const marker = "__GROVE_PATH__"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The marker skips anything an interactive rc file prints first.
	out, _ := exec.CommandContext(ctx, userShell(), "-l", "-i", "-c", `printf '`+marker+`%s' "$PATH"`).Output()
	if _, p, ok := strings.Cut(string(out), marker); ok && strings.TrimSpace(p) != "" {
		_ = os.Setenv("PATH", strings.TrimSpace(p))
	}
}
