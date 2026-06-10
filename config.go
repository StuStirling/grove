package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Workspace is one selectable entry in the switcher.
type Workspace struct {
	Name   string   `toml:"name"`
	Dir    string   `toml:"dir"`
	Panes  []string `toml:"panes"` // one command per pane; empty entry = bare shell
	Branch string   `toml:"-"`     // display-only, set for discovered worktrees
}

// Repo discovers one workspace per git worktree of the repo at Path.
type Repo struct {
	Path         string   `toml:"path"`   // any path inside the repo
	Prefix       string   `toml:"prefix"` // optional name prefix, e.g. "android"
	Panes        []string `toml:"panes"`
	WorktreeRoot string   `toml:"worktree_root"` // base dir for newly created worktrees
	Base         string   `toml:"base"`          // start-point for new branches, default "origin/develop"
	Setup        string   `toml:"setup"`         // command run in the shell pane after creating a worktree
}

// Config is the whole workspaces.toml file.
type Config struct {
	// Terminal is a command template for opening a new terminal window (the -w
	// path). It may contain a {cmd} placeholder, e.g. "ghostty -e {cmd}",
	// "wezterm start -- {cmd}", "kitty {cmd}", "alacritty -e {cmd}".
	Terminal  string      `toml:"terminal"`
	Workspace []Workspace `toml:"workspace"`
	Repo      []Repo      `toml:"repo"`
}

// localConfigName is the repo-local config filename.
const localConfigName = ".grove.toml"

// configPath returns the global ~/.config/grove/workspaces.toml.
func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "grove", "workspaces.toml")
}

// gitRoot walks up from dir to find the repository root (a dir containing .git).
// Returns "" if none is found.
func gitRoot(dir string) string {
	for {
		if fileExists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// findLocalConfig walks up from the current directory looking for .grove.toml,
// not searching above the repository root. Returns "" if none is found.
func findLocalConfig() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if p := filepath.Join(dir, localConfigName); fileExists(p) {
			return p
		}
		if fileExists(filepath.Join(dir, ".git")) {
			return "" // reached repo root without a local config
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// resolveConfigPath returns the config to load: a repo-local .grove.toml when one
// is found (local wins), otherwise the global path.
func resolveConfigPath() (path string, isLocal bool) {
	if p := findLocalConfig(); p != "" {
		return p, true
	}
	return configPath(), false
}

// expandPath replaces a leading ~ with the home directory.
func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// loadConfig reads the resolved config (repo-local .grove.toml wins, else global).
// It does not create anything: a missing config is an error pointing at
// `grove init`.
func loadConfig() (*Config, error) {
	path, _ := resolveConfigPath()
	if !fileExists(path) {
		return nil, fmt.Errorf("no grove config found\n  looked for %s up to the repo root, and %s\n  run `grove init` to create one in this repo",
			localConfigName, path)
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// A repo with no explicit path defaults to the config's own directory, so a
	// repo-local .grove.toml needs no path.
	cfgDir := filepath.Dir(path)
	for i := range cfg.Repo {
		if strings.TrimSpace(cfg.Repo[i].Path) == "" {
			cfg.Repo[i].Path = cfgDir
		}
	}
	for i := range cfg.Workspace {
		cfg.Workspace[i].Dir = expandPath(cfg.Workspace[i].Dir)
	}
	if len(cfg.Workspace) == 0 && len(cfg.Repo) == 0 {
		return nil, fmt.Errorf("no [[workspace]] or [[repo]] defined in %s", path)
	}
	return &cfg, nil
}

// initConfig writes a .grove.toml template at the repo root (or cwd if not in a
// repo). It refuses to overwrite an existing file.
func initConfig() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if root := gitRoot(dir); root != "" {
		dir = root
	}
	target := filepath.Join(dir, localConfigName)
	if fileExists(target) {
		return fmt.Errorf("%s already exists", target)
	}
	if err := os.WriteFile(target, []byte(sampleConfig()), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", target)
	return nil
}

// resolve expands [[repo]] worktrees and merges with manual [[workspace]]
// entries. Repo discovery failures are reported as warnings, not fatal.
func (c *Config) resolve() []Workspace {
	out := append([]Workspace(nil), c.Workspace...)
	for _, r := range c.Repo {
		wss, err := expandRepo(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "grove: repo %s: %v\n", r.Path, err)
			continue
		}
		out = append(out, wss...)
	}
	return out
}

// sampleConfig is the template written by `grove init` into a repo-local
// .grove.toml. The repo's path defaults to this file's directory.
func sampleConfig() string {
	return `# grove repo-local config (.grove.toml).
# panes: one command per pane (empty string = plain shell).

# Command to open a NEW terminal window (only used by 'grove open <name> -w').
# {cmd} is replaced with the attach command. Examples:
#   "ghostty -e {cmd}"  "wezterm start -- {cmd}"  "kitty {cmd}"  "alacritty -e {cmd}"
terminal = ""

# [[repo]] = auto-discover one workspace per git worktree.
[[repo]]
# path        = ""                          # defaults to this repo (this file's dir)
prefix        = ""                          # optional name prefix
panes         = ["claude", "", "lazygit"]   # first = big middle pane; rest = right column
# New-worktree settings (used by the "n" action in the grove pane):
worktree_root = ""                          # REQUIRED for "n": dir for new worktrees, e.g. "~/code/myrepo-worktrees"
base          = "origin/main"               # new branch start-point (fetched first)
setup         = ""                          # e.g. "./scripts/bootstrap.sh" — run in the shell pane after create

# [[workspace]] = a manual one-off entry.
# [[workspace]]
# name   = "notes"
# dir    = "~/notes"
# panes  = [""]
`
}
