# Projects in the sidebar: design

Date: 2026-11-06
Status: approved in conversation, awaiting spec review

## Goal

When one piece of work spans several repos (typically started with ⌘L), show its worktrees together in the sidebar as one project, instead of spread across each repo's group.

**Success:** after starting ENG-412 in android, ios and backend, the sidebar shows an `eng-412` group with all three worktrees and their Claude states, with nothing to set up by hand.

## Decisions

| Decision | Choice | Rejected |
|---|---|---|
| Membership | Derived from the worktree folder name, plus manual overrides | Shared branch name (⌘L now gives each repo its own branch); explicit projects only |
| Manual actions | Add to a project, New project…, Remove from project, on the row menu | Add only (a coincidental name match would stick); full management with rename and delete |
| Storage | `localStorage`, keyed by worktree dir | A file written by Go. Collapsed groups, split ratios and the ⌘L repo set already live in `localStorage` |
| Layout | Project groups on top, their worktrees moved out of the repo groups (prototype option A) | A Repos / Projects switch (B); a chip on rows in place (C) |

## Membership

- **Derived:** worktrees with the same folder name (the last element of `dir`) in two or more of the window's repos form a project named after that folder. This is the rule `siblings()` in `removal.ts` already uses. A repo's main checkout never counts. It is the repo's first worktree in the snapshot, because `git worktree list` lists it first.
- **Overrides:** `grove.projects` in `localStorage` maps a worktree dir to a project name (put it in that project, whatever its folder is called) or to `null` (never put it in a project). An override beats the folder-name rule.
- **Size:** a project created by an override may have a single member. A derived project needs members in two or more repos, counted after `null` overrides are applied. When it drops below that, the group dissolves and its remaining worktree goes back to its repo group.
- **Pruning:** overrides for dirs no longer in the snapshot are dropped when a snapshot arrives. This applies only once a snapshot has loaded, never to an empty or failed one.

Renaming and deleting a project are out of scope. A project goes away when its members are removed from it or deleted.

## Sidebar

```
ENG-412                 ⋯
 ●◆ android             ⌘1
    f/eng-412-cache-avatars
 ○◌ ios                 ⌘2
    f/eng-412-cache-avatars
 ○  backend             ⌘3
    f/eng-413-avatar-cdn
ANDROID                 ⋯
    android [main]      ⌘4
    develop
```

- **Order:** project groups first, sorted by name; then the repo groups in the window's order, without the project members; then "Other" (manual `[[workspace]]` entries, which are never in a project).
- **Project header:** the project name in the group header style, coloured with `--idle` so it reads differently from a repo header. Its ⋯ menu has only Collapse / Expand. Collapse state goes in the existing `collapsed` list as `project:<name>`, so the "N hidden" tag works too.
- **Rows in a project:** the name line shows the repo's name (`w.repo`). When the folder differs from the project name (a worktree added by override), it shows `<repo> · <folder>`. The branch line, status gutter, Claude marks and removal states don't change.
- **Filter:** a row also matches when its project name contains the query, so typing `eng-412` lists the whole group.
- **Row menu:** after Copy branch and Copy path, a divider, then:
  - **Add to <project>**, one item per current project except the row's own. `Menu` has no submenus, and a window rarely has more than a few projects, so they are listed inline.
  - **New project…**, which opens a small dialog with a name field, in the style of grove's other dialogs. Esc cancels; an empty name, or one that is already a project, keeps Create disabled.
  - **Remove from project**, only on rows in one. It sets the row's override to `null`.
- **Main checkout:** each repo's main checkout gets a `main` tag after its name, styled like the `added` tag on repo headers (`--muted` text, a `--border-strong` border, 10px). It shows wherever the row does, a project group or a filtered list included. A main checkout can still be added to a project by override; it just never joins one by folder name.

## ⌘1–9

Today ⌘N opens `all[n]` and the sidebar numbers rows by `all.indexOf(w)`. That matches the list only because repo groups follow snapshot order. With projects on top it would not. Both will use one `numbered` list: the rows in display order with nothing collapsed and no filter. A worktree hidden in a collapsed group keeps its number but doesn't open. The existing "expand X to open Y" message names the project or repo it is hidden in.

## Code

All frontend. No Go or binding changes.

- **`projects.ts` (new, pure):**
  - `type Overrides = Record<string, string | null>`
  - `membership(all, overrides): Map<string, string>`, dir to project name.
  - `prune(overrides, all): Overrides`.
  - `mains(all): Set<string>`, the dirs of each repo's main checkout (its first row in the snapshot). Used by `membership` and by the `main` tag.
  - `loadOverrides()` / `saveOverrides()`, wrapping `localStorage` in try/catch like the other keys.
- **`repos.ts`:** `Group` becomes `{ repo: RepoInfo | null; project?: string; rows }`. `groups()` takes the membership map and builds project groups first. `matches()` also takes the row's project name. It exports `numbered(...)` for the ⌘1–9 order.
- **`App.tsx`:** holds the overrides state, prunes it on each snapshot, passes membership into `groups()`, switches ⌘1–9 to `numbered`, and handles the New project… dialog.
- **`Sidebar.tsx`:** project headers and their menu, row names inside projects, the row-menu items, the `main` tag, numbering from `numbered`.
- **`style.css`:** the project header colour and the `main` tag.

## Testing

- **`projects.test.ts`:**
  - same folder in two repos is a project; in one repo is not
  - the main checkout is excluded, and `mains` picks each repo's first row
  - an override adds a worktree with a different folder name
  - a `null` override removes a derived member, and a derived project left with one repo dissolves
  - a one-member override project stays
  - pruning drops missing dirs and keeps the rest
- **`repos.test.ts`:**
  - project groups come first, sorted
  - members are left out of their repo groups
  - filtering by project name keeps the whole group
  - a collapsed project lists no rows
  - `numbered` follows display order regardless of collapse

## Out of scope

- Renaming or deleting a project
- Changing how deleting a worktree offers its siblings (still by folder name)
- Projects across windows. Overrides are keyed by dir, so a worktree's override follows it into any window that shows it.
- Collapsing Claude Code's `.claude/worktrees/*`, which is still queued separately
