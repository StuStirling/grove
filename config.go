package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Workspace is one selectable entry in the switcher.
type Workspace struct {
	Name     string   `toml:"name"`
	Dir      string   `toml:"dir"`
	Panes    []string `toml:"panes"` // one command per pane; empty entry = bare shell
	Branch   string   `toml:"-"`     // display-only, set for discovered worktrees
	RepoPath string   `toml:"-"`     // owning repo path, set for discovered worktrees; "" for manual entries
	RepoName string   `toml:"-"`     // display repo name (prefix, else repo dir basename); drives the terminal title
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
	// Terminal is a command template for opening a worktree in an external
	// terminal window ("Open in Terminal", `grove open -w`). {cmd} is replaced
	// with a shell started in the worktree and {dir} with the worktree path, e.g.
	// "ghostty -e {cmd}", "wezterm start -- {cmd}", "kitty {cmd}".
	Terminal   string      `toml:"terminal"`
	FontFamily string      `toml:"font_family"` // terminal font; empty = system monospace
	FontSize   int         `toml:"font_size"`   // terminal font size in px; 0 = default
	Workspace  []Workspace `toml:"workspace"`
	Repo       []Repo      `toml:"repo"`

	// Path is the config file this was loaded from. It keys the GUI instance, so
	// every grove process for the same repo finds the same running window.
	Path string `toml:"-"`
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

// mainWorktreeConfig returns the .grove.toml of the repository's MAIN worktree,
// or "". This lets linked worktrees (which each have their own .git file and no
// committed config) share the config that lives in the main worktree, so `grove`
// run from any worktree (or from a pane) finds the same config and window.
func mainWorktreeConfig() string {
	out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		// Older git without --path-format: fall back and resolve manually.
		out, err = exec.Command("git", "rev-parse", "--git-common-dir").Output()
		if err != nil {
			return ""
		}
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return ""
	}
	if !filepath.IsAbs(commonDir) {
		if cwd, e := os.Getwd(); e == nil {
			commonDir = filepath.Join(cwd, commonDir)
		}
	}
	// commonDir is the shared ".../<mainRoot>/.git"; its parent is the main root.
	p := filepath.Join(filepath.Dir(commonDir), localConfigName)
	if fileExists(p) {
		return p
	}
	return ""
}

// resolveConfigPath returns the config to load: a repo-local .grove.toml when one
// is found (local wins), then the main worktree's config, otherwise global.
func resolveConfigPath() (path string, isLocal bool) {
	if p := findLocalConfig(); p != "" {
		return p, true
	}
	if p := mainWorktreeConfig(); p != "" {
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
	cfg.Path = path

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

// socketPath derives the stable, per-config IPC socket of the GUI instance.
// Every grove process for the same repo resolves to the same config path (linked
// worktrees fall back to the main worktree's config), so they reach the same
// window; different repos hash to different sockets and run concurrently.
func (c *Config) socketPath() string {
	abs, err := filepath.Abs(c.Path)
	if err != nil {
		abs = c.Path
	}
	// Canonicalise symlinks so a launcher resolving via os.Getwd and one resolving
	// via git --git-common-dir hash to the same socket.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	sum := sha256.Sum256([]byte(abs))
	// Only the hash goes in the name: unix socket paths are capped at ~104 bytes.
	return filepath.Join(socketDir(), fmt.Sprintf("grove-%x.sock", sum[:4]))
}

// socketDir holds the per-repo GUI sockets. The user cache dir is stable across
// launch contexts (terminal, Finder), unlike TMPDIR.
func socketDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "grove")
	}
	return os.TempDir()
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

# Command to open a worktree in an external terminal ("Open in Terminal",
# 'grove open <name> -w'). {cmd} = a shell in the worktree, {dir} = its path:
#   macOS: "open -na Ghostty --args --working-directory={dir}"
#   Linux: "ghostty -e {cmd}"  "wezterm start -- {cmd}"  "kitty {cmd}"  "alacritty -e {cmd}"
terminal = ""

# Terminal font for the panes (defaults: system monospace, 13px).
# font_family = "JetBrains Mono"
# font_size   = 13

# [[repo]] = auto-discover one workspace per git worktree.
[[repo]]
# path        = ""                          # defaults to this repo (this file's dir)
prefix        = ""                          # optional name prefix
panes         = ["claude", ""]              # the tabs a worktree opens with ("" = your shell)
# New-worktree settings (used by New Worktree, cmd-N):
worktree_root = ""                          # REQUIRED to create: dir for new worktrees, e.g. "~/code/myrepo-worktrees"
base          = "origin/main"               # new branch start-point (fetched first)
setup         = ""                          # e.g. "./scripts/bootstrap.sh": run in the shell pane after create

# [[workspace]] = a manual one-off entry.
# [[workspace]]
# name   = "notes"
# dir    = "~/notes"
# panes  = [""]
`
}
