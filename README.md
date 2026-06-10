# grove

A tmux-backed **git-worktree switcher**. `grove` lists your repo's worktrees,
opens each as a tmux window with a fixed layout (a slim switcher pane + your
tools), jumps between them instantly, and creates new worktrees on the fly.

```
┌───────┬──────────┬─────────┐
│ grove │  claude  │  shell  │   left:   grove switcher (in every window)
│ list  │  (big)   ├─────────┤   middle: first configured pane
│       │          │ lazygit │   right:  the rest
└───────┴──────────┴─────────┘
```

It's **terminal-agnostic** — tmux does the work, so grove runs in any terminal
(ghostty, wezterm, kitty, alacritty, iTerm, …). Works on macOS and Linux —
anywhere tmux runs.

## Requirements

- [tmux](https://github.com/tmux/tmux)
- git
- whatever you put in `panes` (e.g. `claude`, `lazygit`)

## Install

Homebrew:

```sh
brew install stustirling/tap/grove
```

Go:

```sh
go install github.com/StuStirling/grove@latest
```

## Quick start

```sh
cd your-repo
grove init        # writes a .grove.toml template at the repo root
$EDITOR .grove.toml
grove             # pick a worktree → attaches in the current tab
```

The left **grove** pane is the switcher. It appears in every worktree window, so
switching feels seamless and each worktree keeps its own panes running.

## Commands

```
grove                        launch the TUI switcher (attaches in this tab)
grove open <name>            attach a workspace in the current tab
grove open <name> -w         open the workspace in a new terminal window
grove new <intention> <br>   create a worktree (branch <br>) and open it
grove init                   write a .grove.toml template in the current repo
grove list                   print workspace names
grove doctor                 check prerequisites (tmux, git)
grove version                print the version
```

### TUI keys

```
↑/↓ move   ⏎ open   n new   x close   r reload   q quit
```

`n` opens a form: **intention** (worktree dir name), **branch**, and **base**
(prefilled from config; type to autocomplete against the repo's branches, `→`
to accept). `x` closes a worktree's window (the worktree on disk is untouched).

## Configuration

`grove` loads a repo-local `.grove.toml` (searched upward to the repo root); if
none is found it falls back to the global `~/.config/grove/workspaces.toml`.

```toml
# Only needed for `grove open <name> -w` (new window). {cmd} is replaced with
# the attach command. Examples per terminal:
#   "ghostty -e {cmd}"   "wezterm start -- {cmd}"   "kitty {cmd}"   "alacritty -e {cmd}"
terminal = "ghostty -e {cmd}"

[[repo]]
# path        = ""                       # defaults to this repo (the config's dir)
prefix        = ""                        # optional name prefix
panes         = ["claude", "", "lazygit"] # first = big middle pane; rest = right column
worktree_root = "~/code/myrepo-worktrees" # where `new` creates worktrees (<root>/<intention>)
base          = "origin/main"             # new-branch start-point (fetched if it's a remote ref)
setup         = "./scripts/bootstrap.sh"  # optional: run in the shell pane after `new`
```

`panes` is one command per pane (`""` = a plain shell). `grove` adds the slim
switcher pane on the left automatically.

## How it works

Each worktree is a tmux **window** in one shared session (`grove`). The default
flow replaces the current process with `tmux attach`/`switch-client`, so it
takes over the current tab in any terminal. The only terminal-specific action is
`-w` (open a brand-new window), which runs the configurable `terminal` command.

## Development

Dogfood it while you build it — install to a directory on your `PATH` and rebuild
as you go:

```sh
make install        # builds with version=git-describe -> ~/.local/bin/grove
# (override the dir: make install BINDIR=~/bin)
```

`grove` embeds itself as the left switcher pane (via the running binary's path),
so a rebuild is picked up by **new** windows automatically. To refresh the
switcher pane in already-open windows, restart the session:

```sh
tmux kill-server    # then run `grove` again
```

`make test` / `make vet` / `make fmt` for the usual checks.

## Licence

MIT — see [LICENSE](LICENSE).
