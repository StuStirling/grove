# grove

A **git-worktree switcher** with a GUI. `grove` lists your repo's worktrees,
opens each one as a set of terminal panes with a fixed layout (your big pane,
plus your tools), switches between them instantly, and creates new worktrees on
the fly. Everything is driven from the keyboard.

```
┌──────────────┬──────────────┬─────────┐
│ worktrees    │   claude     │   zsh   │   left:   worktree list (⌘P to jump)
│ ● ◆ feature  │   (big)      │         │   middle: first configured pane
│ ○   main     │              │         │   right:  the rest (stacked if several)
└──────────────┴──────────────┴─────────┘
```

Each worktree keeps its panes running in the background while you work in
another. The sidebar shows which worktrees are open and which one's Claude Code
is waiting for you. Works on macOS and Linux.

## Requirements

- macOS 12+ or Linux
- git
- whatever you put in `panes` (e.g. `claude`)
- Linux only: WebKitGTK 4.1 (`libwebkit2gtk-4.1-0` on Debian/Ubuntu)

## Install

**Homebrew (macOS)** installs `grove.app` to `/Applications` and `grove` onto your
PATH:

```sh
brew install --cask stustirling/tap/grove
```

**Release download:** grab `grove-darwin-universal.zip` (macOS) or
`grove_<version>_linux_<arch>.tar.gz` from
[Releases](https://github.com/StuStirling/grove/releases). The macOS app is
unsigned, so after unzipping it into `/Applications` clear the quarantine flag
once (Homebrew does this for you):

```sh
xattr -dr com.apple.quarantine /Applications/grove.app
ln -sf /Applications/grove.app/Contents/MacOS/grove ~/.local/bin/grove   # the CLI
```

**From source** needs Go 1.25+, Node 22+ and the [Wails](https://wails.io) CLI
(`go install github.com/wailsapp/wails/v2/cmd/wails@latest`). On Linux also
`sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev` (or your distro's
equivalents).

```sh
make install        # macOS: grove.app -> /Applications, `grove` -> ~/.local/bin
                    # Linux: `grove` -> ~/.local/bin
```

Make sure `~/.local/bin` is on your PATH. Override the locations with
`make install APPDIR=~/Applications BINDIR=~/bin`.

## Quick start

```sh
cd your-repo
grove init          # writes a .grove.toml template at the repo root
$EDITOR .grove.toml # set worktree_root (needed to create worktrees) and panes
grove               # opens this repo's grove window
```

For the Claude Code marks in the sidebar, add the hooks from
[Claude Code status](#claude-code-status).

Run `grove` from any worktree of the repo and you get the same window: it is one
window per repo. Different repos get their own windows and run side by side.

Opened from Finder, the Dock or Spotlight there is no repo to start in, so grove
uses the global `~/.config/grove/workspaces.toml` if you have one, and otherwise
offers **Open Repository… (⌘O)** to pick a repo folder. ⌘O works from any
window to open (or switch to) another repo's window.

## Commands

```
grove                        open this repo's grove window (focuses it if open)
grove open <name>            open a workspace in the grove window
grove open <name> -w         open a workspace in an external terminal (uses `terminal`)
grove new <intention> <br> [base]
                             create a worktree (new branch <br> from base) and open it
grove remove <name>          remove a worktree (--force if dirty, --branch to delete its branch)
grove init                   write a .grove.toml template in the current repo
grove list                   print workspace names
grove state <state>          report Claude Code state from a pane: working|waiting|idle|clear
grove gui [name]             run the window in the foreground (for debugging)
grove doctor                 check prerequisites
grove version                print the version
grove help                   show this help
```

## Keyboard shortcuts

Shortcuts are menu commands, so they work while a terminal pane has focus.
Press **⌘/** in the app for the full list. On Linux, ⌘ is **Ctrl+Shift** (plus
**Alt** where the Mac shortcut already uses ⇧).

| Worktrees | | Panes | |
|---|---|---|---|
| Go to worktree (type to filter, ↑↓ ↩) | ⌘P | Next / previous pane | ⌘] / ⌘[ |
| Worktree 1–9 | ⌘1–⌘9 | Pane left / right / above / below | ⌘⌥←→↑↓ |
| Next / previous open worktree | ⌘⇧] / ⌘⇧[ | Zoom pane | ⌘⇧↩ |
| New worktree… | ⌘N | Add shell pane | ⌘D |
| New worktree from branch… | ⌘⇧N | Bigger / smaller / actual text | ⌘= / ⌘- / ⌘0 |
| Close worktree (stop its panes) | ⌘W | Newline in Claude Code | ⇧↩ |
| Delete worktree… | ⌘⌫ | Open link | ⌘-click |
| Reload worktrees | ⌘R | Select over a mouse-aware app | ⌥-drag |
| Open in external terminal | ⌘⇧T | Keyboard shortcuts | ⌘/ |
| Open repository (its own window) | ⌘O | | |

While the worktree list has focus (⌘P), ⌘W / ⌘⌫ / ⌘⇧T act on the highlighted
worktree; otherwise they act on the one you're in. Dialogs take **Y / N**, ↩ and
Esc; deleting needs an explicit **Y**. After that one confirm, a delete shows its
progress in the worktree's own row, and several can run at once. If the worktree
has uncommitted changes the row says how many and offers **Force remove** or
**Keep**. Once it is removed, grove deletes its branch if it is merged; an
unmerged branch is kept, and the row offers **Delete branch** (`git branch -D`,
after a confirm) until you dismiss it.

New Worktree asks for an **intention** (the worktree dir name), a **branch** and a
**base** (prefilled from config, autocompleted from the repo's branches). New
Worktree from Branch checks out an existing local or remote branch. Both run the
repo's `setup` command in the shell pane.

## Claude Code status

The sidebar marks a worktree when its Claude Code needs you: **◆ yellow** for a
permission prompt, **◆ purple** when it has finished, **◌** while it works. The
marks come from Claude Code hooks calling `grove state`. Panes export `GROVE_BIN`
(the running grove's own binary), so the hooks reach the right grove whatever is
on your PATH, and do nothing outside a grove pane. Merge this into the `hooks` of
`~/.claude/settings.json` (alongside any hooks you already have):

```json
{
  "hooks": {
    "SessionStart":      [{ "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state idle || true" }] }],
    "UserPromptSubmit":  [{ "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state working || true" }] }],
    "Notification":      [{ "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state waiting || true" }] }],
    "PermissionRequest": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state waiting || true" }] }],
    "Stop":              [{ "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state idle || true" }] }],
    "SessionEnd":        [{ "hooks": [{ "type": "command", "command": "[ -n \"$GROVE_BIN\" ] && \"$GROVE_BIN\" state clear || true" }] }]
  }
}
```

## Configuration

`grove` loads a repo-local `.grove.toml` (searched upward to the repo root; linked
worktrees use the main worktree's). If none is found it falls back to the global
`~/.config/grove/workspaces.toml`.

```toml
# Open a worktree in an external terminal (⌘⇧T, `grove open <name> -w`).
# {cmd} = a shell started in the worktree, {dir} = the worktree path.
#   macOS: "open -na Ghostty --args --working-directory={dir}"
#          "open -na WezTerm --args start --cwd {dir}"
#   Linux: "ghostty -e {cmd}"  "wezterm start -- {cmd}"  "kitty {cmd}"  "alacritty -e {cmd}"
terminal = "open -na Ghostty --args --working-directory={dir}"

font_family = "JetBrains Mono"            # pane font (default: system monospace)
font_size   = 13                          # pane font size in px

[[repo]]
# path        = ""                       # defaults to this repo (the config's dir)
prefix        = ""                        # optional name prefix
panes         = ["claude", ""]            # first = big pane; rest = right column ("" = your shell)
worktree_root = "~/code/myrepo-worktrees" # where New Worktree creates <root>/<intention>
base          = "origin/main"             # new-branch start-point (fetched if it's a remote ref)
setup         = "./scripts/bootstrap.sh"  # optional: run in the shell pane after creating

# [[workspace]] = a manual one-off entry (not a git worktree, so not deletable).
# [[workspace]]
# name  = "notes"
# dir   = "~/notes"
# panes = [""]
```

`panes` is one command per pane (`""` = your login shell: `$SHELL`, zsh on macOS
by default). Extra panes stack in the right column, e.g.
`["claude", "", "lazygit"]`. When a pane's program exits its pane closes, and a
worktree closes with its last pane. Config is read when the window opens; restart
grove after editing it.

## How it works

Each pane is a real terminal (a pty running the command through your `$SHELL`,
drawn with xterm.js) owned by the grove window. Panes get `TERM=xterm-256color`
and `GROVE_PANE` / `GROVE_SOCK` / `GROVE_WORKSPACE` / `GROVE_BIN`, and none of the
launching terminal's identity (no `TMUX`, `TERM_PROGRAM`, …) or of a Claude Code
session grove was started from, so `claude` in a pane is a normal session.

Every grove process for a repo talks to that repo's window over a unix socket
under your cache dir. That is how `grove`, `grove open`, `grove new` and
`grove remove` reach a running window, and how `grove state` marks the sidebar.
`grove` from a terminal hands off to the window and returns your prompt.

Quitting grove stops every pane, so it asks first while any are running.

## Upgrading from the tmux version

grove no longer uses tmux. After upgrading:

- Remove the old tmux `@claude_state` hooks from `~/.claude/settings.json` and
  add the [grove ones](#claude-code-status).
- Existing `grove-*` tmux sessions keep running until you end them
  (`tmux kill-session -t <name>`); grove won't reattach to them.
- `grove open <name> -w` and `terminal` now open the worktree in an external
  terminal (`{cmd}` / `{dir}`) rather than attaching tmux.

## Development

```sh
make dev            # wails dev: live-reloading frontend + Go backend
make build          # build/bin/grove.app (macOS) or build/bin/grove (Linux)
make install        # build with version=git-describe and install (see above)
make test           # builds the frontend (the binary embeds it), then go test
make vet / make fmt
```

The backend is Go (`app.go` bindings and menu, `session.go` ptys, `ipc.go`
socket, `worktree.go` git). The frontend is React + TypeScript in `frontend/`;
`frontend/wailsjs` holds the generated bindings (`wails generate module`).

## Licence

MIT: see [LICENSE](LICENSE).
