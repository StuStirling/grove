package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The json names are the settings dialog's, which edits the file as written.

// Workspace is one selectable entry in the switcher.
type Workspace struct {
	Name     string   `toml:"name" json:"name"`
	Dir      string   `toml:"dir" json:"dir"`
	Panes    []string `toml:"panes,omitempty" json:"panes"` // one command per pane; empty entry = bare shell
	Branch   string   `toml:"-" json:"-"`                   // display-only, set for discovered worktrees
	RepoPath string   `toml:"-" json:"-"`                   // owning repo path, set for discovered worktrees; "" for manual entries
	RepoName string   `toml:"-" json:"-"`                   // display repo name (prefix, else repo dir basename); drives the terminal title
	Config   string   `toml:"-" json:"-"`                   // path of the config file it came from, which a window adds and removes whole
}

// Repo discovers one workspace per git worktree of the repo at Path.
type Repo struct {
	Path         string   `toml:"path,omitempty" json:"path"`     // any path inside the repo
	Prefix       string   `toml:"prefix,omitempty" json:"prefix"` // optional name prefix, e.g. "android"
	Panes        []string `toml:"panes,omitempty" json:"panes"`
	WorktreeRoot string   `toml:"worktree_root,omitempty" json:"worktreeRoot"` // base dir for newly created worktrees
	Base         string   `toml:"base,omitempty" json:"base"`                  // start-point for new branches, default "origin/develop"
	Setup        string   `toml:"setup,omitempty" json:"setup"`                // command run in the shell pane after creating a worktree
	Config       string   `toml:"-" json:"-"`                                  // path of the config file it came from
}

// Config is the whole workspaces.toml file.
type Config struct {
	// Terminal is a command template for opening a worktree in an external
	// terminal window ("Open in Terminal", `grove open -w`). {cmd} is replaced
	// with a shell started in the worktree and {dir} with the worktree path, e.g.
	// "ghostty -e {cmd}", "wezterm start -- {cmd}", "kitty {cmd}".
	Terminal   string      `toml:"terminal,omitempty" json:"terminal"`
	FontFamily string      `toml:"font_family,omitempty" json:"fontFamily"` // terminal font; empty = system monospace
	FontSize   int         `toml:"font_size,omitzero" json:"fontSize"`      // terminal font size in px; 0 = default
	Repo       []Repo      `toml:"repo" json:"repo"`
	Workspace  []Workspace `toml:"workspace" json:"workspace"`

	// Path is the config file this was loaded from. It keys the GUI instance, so
	// every grove process for the same repo finds the same running window.
	Path string `toml:"-" json:"-"`
	// Missing: there is no file at Path yet. A repo with no config is set up
	// from the window, which asks for what it needs and writes Path.
	Missing bool `toml:"-" json:"-"`
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

// findLocalConfig walks up from dir looking for .grove.toml, not searching above
// the repository root. Returns "" if none is found.
func findLocalConfig(dir string) string {
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

// mainWorktreeRoot returns the root of the repository's MAIN worktree, or "" if
// dir is not in a repo. Linked worktrees (which each have their own .git file
// and no committed config) share the config that lives there, so `grove` run
// from any worktree (or from a pane) finds the same config and window.
func mainWorktreeRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		// Older git without --path-format: fall back and resolve manually.
		out, err = exec.Command("git", "-C", dir, "rev-parse", "--git-common-dir").Output()
		if err != nil {
			return ""
		}
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return ""
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(dir, commonDir)
	}
	// commonDir is the shared ".../<mainRoot>/.git"; its parent is the main root.
	root := filepath.Dir(commonDir)
	if !fileExists(filepath.Join(root, ".git")) {
		return gitRoot(dir) // a submodule's (.git/modules/x): no main worktree above it
	}
	return root
}

// mainWorktreeConfig returns the .grove.toml of the repository's main worktree,
// or "".
func mainWorktreeConfig(dir string) string {
	if root := mainWorktreeRoot(dir); root != "" {
		if p := filepath.Join(root, localConfigName); fileExists(p) {
			return p
		}
	}
	return ""
}

// resolveConfigPath returns the config for dir: a repo-local .grove.toml when one
// is found (local wins), then the main worktree's config, otherwise global.
func resolveConfigPath(dir string) (path string, isLocal bool) {
	if p := findLocalConfig(dir); p != "" {
		return p, true
	}
	if p := mainWorktreeConfig(dir); p != "" {
		return p, true
	}
	return configPath(), false
}

// tildePath writes a path under the home directory as ~/..., as a config would.
func tildePath(p string) string {
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// defaultWorktreeRoot is where a repo's new worktrees go when worktree_root is
// unset: <repo>-worktrees beside it.
func defaultWorktreeRoot(repoPath string) string {
	return filepath.Join(filepath.Dir(repoPath), filepath.Base(repoPath)+"-worktrees")
}

// expandPath replaces a leading ~ with the home directory.
func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// loadConfig reads the config resolved from the current directory (repo-local
// .grove.toml wins, else global). In a repo with neither, it is the repo's
// .grove.toml to be, Missing, for the window to set up. Outside a repo a
// missing config is an error.
func loadConfig() (*Config, error) {
	wd, _ := os.Getwd()
	path, _ := resolveConfigPath(wd)
	if fileExists(path) {
		return readConfig(path)
	}
	if root := mainWorktreeRoot(wd); root != "" {
		return &Config{Path: filepath.Join(root, localConfigName), Missing: true}, nil
	}
	return nil, fmt.Errorf("no grove config found\n  looked for %s up to the repo root, and %s\n  run `grove` inside a git repo to set one up",
		localConfigName, path)
}

// readConfig parses the config file at path.
func readConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	cfg.Path = path

	// A repo's path is relative to the config's own directory, not the process
	// cwd, and defaults to it, so a repo-local .grove.toml needs no path.
	cfgDir := filepath.Dir(path)
	for i := range cfg.Repo {
		if p := expandPath(strings.TrimSpace(cfg.Repo[i].Path)); filepath.IsAbs(p) {
			cfg.Repo[i].Path = p
		} else {
			cfg.Repo[i].Path = filepath.Join(cfgDir, p)
		}
		cfg.Repo[i].Config = path
	}
	for i := range cfg.Workspace {
		cfg.Workspace[i].Dir = expandPath(cfg.Workspace[i].Dir)
		cfg.Workspace[i].Config = path
	}
	if len(cfg.Workspace) == 0 && len(cfg.Repo) == 0 {
		return nil, fmt.Errorf("no [[workspace]] or [[repo]] defined in %s", path)
	}
	return &cfg, nil
}

// writeConfig writes cfg to the config file at path, as plain TOML: comments in
// the file are not kept. It writes through a symlink (a dotfiles-managed
// config), and atomically, so a failed write leaves the file as it was.
func writeConfig(path string, cfg Config) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	var b bytes.Buffer
	enc := toml.NewEncoder(&b)
	enc.Indent = ""
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// fileVersion identifies a config file's contents, so a save can tell the file
// changed on disk since it was read.
func fileVersion(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

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
	// via git --git-common-dir hash to the same socket. A Missing config resolves
	// by its dir, as it will once it is written.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	} else if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		abs = filepath.Join(dir, filepath.Base(abs))
	}
	sum := sha256.Sum256([]byte(abs))
	// Only the hash goes in the name: unix socket paths are capped at ~104 bytes.
	return filepath.Join(socketDir(), fmt.Sprintf("grove-%x.sock", sum[:4]))
}

// reposFile lists the other repos this config's window also shows (App.addRepo).
func (c *Config) reposFile() string { return strings.TrimSuffix(c.socketPath(), ".sock") + ".repos" }

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

// resolve expands [[repo]] worktrees, repo by repo, then adds the manual
// [[workspace]] entries: the order the sidebar groups them in. A manual entry
// whose dir is already listed (a repo's worktree, or the same entry in another
// config shown in the window) is dropped, since dirs key workspaces. Repo
// discovery failures are reported as warnings, not fatal.
func (c *Config) resolve() []Workspace {
	var out []Workspace
	for _, r := range c.Repo {
		wss, err := expandRepo(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "grove: repo %s: %v\n", r.Path, err)
			continue
		}
		out = append(out, wss...)
	}
	for _, w := range c.Workspace {
		if !slices.ContainsFunc(out, func(o Workspace) bool { return o.Dir == w.Dir }) {
			out = append(out, w)
		}
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
panes         = ["claude", ""]              # first = left pane; rest = tabs in a right pane ("" = your shell)
# New-worktree settings (used by New Worktree, cmd-N):
# worktree_root = ""                        # dir for new worktrees; default: <repo>-worktrees beside the repo
# base          = "origin/main"             # new branch start-point (fetched first); default: the remote's default branch
setup         = ""                          # e.g. "./scripts/bootstrap.sh": run in the shell pane after create

# [[workspace]] = a manual one-off entry.
# [[workspace]]
# name   = "notes"
# dir    = "~/notes"
# panes  = [""]
`
}
