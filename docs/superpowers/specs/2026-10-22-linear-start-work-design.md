# Start work from Linear: design

Date: 2026-10-22
Status: approved in conversation, awaiting spec review
Prototype: https://claude.ai/artifact/5Z4ysSeNUzPNEEomonZMwv (rows 1 to 3; row 4 is the rejected coordinator alternative)

## Goal

Find a Linear issue or project from inside grove and, in a few keystrokes, get worktrees in one or more of the window's repos with a Claude Code session already planning the work.

**Success:** from ⌘L to a lead Claude in plan mode, seeded with the ticket, without copying a branch name, identifier or description by hand.

## Decisions

| Decision | Choice | Rejected |
|---|---|---|
| Integration | grove calls Linear's GraphQL API itself | Delegating to Claude's Linear MCP (no search, no Linear branch names); a "paste a URL" field in New Worktree (keeps the copying) |
| Auth | Personal API key, read access, in the OS keychain | OAuth app; key in `.grove.toml` (committed); env var |
| Linear writes | None. grove only reads | Moving status, posting comments |
| After create | The lead's Claude starts in plan mode, seeded with the ticket | Normal mode; worktrees only |
| Multi-repo | One lead worktree; its Claude gets `--add-dir` for the others | Independent seeded sessions per repo; a coordinator session in a folder outside every repo (loses the lead repo's auto memory, settings, hooks and project MCP servers) |
| Projects | A project is a unit of work: the lead is seeded with the project and all its issues and asked for an end-to-end plan | Browsing into issues only |
| Other repos' instructions | `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` on the lead, so `CLAUDE.md`, `.claude/rules/*.md` and `CLAUDE.local.md` load from every `--add-dir` worktree | Lead repo's instructions only |
| Shortcut | ⌘L (File ▸ Start Work from Linear…) | ⌘⇧L, ⌘K |
| Default repos | The set ticked last time; the first time, the current worktree's repo | Current repo only; a set per Linear team |
| Project issues in the prompt | All, with done and cancelled ones marked as such | Open issues only |

## User flow

### 1. Connect (first use)

⌘L with no key saved opens **Connect Linear**: where to create a personal API key (Linear ▸ Settings ▸ Account ▸ Security & access, Read access), a password field, Cancel and Connect. Connect calls `viewer` to check the key, then saves it to the keychain. Settings (⌘,) gets a **Linear** section: "Connected as <name>" and **Disconnect**, or **Connect…** when there is no key.

### 2. Search palette

A dialog in the style of grove's others, opened by ⌘L.

- **Empty query:** "Assigned to you" (issues assigned to the viewer whose state type isn't completed or canceled, most recently updated first, up to 50) and "Your projects" (projects the viewer leads or is a member of, status not completed or canceled, up to 20).
- **Typed query:** "Issues" and "Projects" from `searchIssues` and `searchProjects`, 20 and 10 results, after a 400ms pause in typing. Linear rate-limits each search to 30 requests a minute. A query matching `^[A-Za-z]+-\d+$` is fetched with `issue(id:)` instead, which is not search-limited.
- **Scope chips:** All, Issues, Projects.
- **Rows:** issues show a state glyph, identifier, title, project (or team), state name. Projects show name, `% done` (Linear's `progress`) and target date, status. Counting a project's issues would mean fetching them for every row.
- **Existing work:** an issue whose `branchName` is checked out in a worktree of any window repo shows "worktree in <repo>", and ↩ opens that worktree instead of starting work.
- **Keys:** ↑↓ select, ↩ start work (or go to worktree), ⌘↩ open in Linear, Esc close.

### 3. Start work form

- **Ticket card:** identifier, state, project, title, and the first lines of the description. For a project: status, counts, target date, and every issue with its state, done ones marked. "Open in Linear ↗".
- **Repositories:** every repo in the window, each with a checkbox and a **lead** radio. The lead radio is disabled on unticked repos. If the lead is unticked, the lead moves to the first ticked repo. Each row shows its base and the worktree path, and its `PlanRepo` outcome: will create, will check out existing branch, will reuse existing worktree, or the conflict.
- **Branch:** an issue's Linear `branchName`. For a project: `<prefix>/<project-slug>`, where `<prefix>` is the part before the first `/` of any of the project's issue `branchName`s (Linear personalises these to the viewer), else just the slug. Editable.
- **Worktree name:** the identifier lowercased (`eng-412`) or the project slug. Editable.
- **Start Claude in plan mode** checkbox, on by default, with a collapsible preview of the exact command and prompt.
- **Create N worktrees** is disabled while no repo is ticked or any ticked repo has a conflict.

The ticked set is saved to `localStorage` on Create.

### 4. Creating

The form turns into one progress row per repo.

- The non-lead repos are started in parallel first. Each row ends as created, checked out, reused, or failed with git's reason and **Retry** (same branch and path) or **Skip**.
- Once every non-lead repo is done or skipped, the lead is started with its seed, so `--add-dir` only lists worktrees that exist.
- Worktrees already created stay if the dialog is closed.

### 5. Result

Every new worktree opens, with its repo's `setup` typed into its shell pane, as ⌘N does. The lead is selected and its Claude starts with the seed. If the lead's worktree was already open (reused), the seeded session opens as a new Claude tab in it instead.

## The seed

The lead's Claude command is the lead repo's first Claude entry in `panes`, flags included, else `claude` (the rule ⌘T uses). grove appends:

```
--permission-mode plan [--add-dir "$GROVE_SEED_DIR_1" ...] "$GROVE_SEED_PROMPT"
```

with the values in the pane's environment, so no ticket text is ever parsed by the user's shell (fish, for one, quotes differently from sh). It also sets `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` in that environment when there is at least one `--add-dir`. The pane's visible command stays as configured, so its tab name and Claude status marks work as usual.

### Issue prompt

```
Plan the work for Linear issue <ID>: <title>
<url>

<location>

Plan across every repo before changing anything.     (multi-repo only)

## Description
<description>
```

`<location>` is, for one repo: "You're in the <repo> worktree on branch <branch>." For several:

```
You're in the <lead> worktree. The work spans N repos, each with a worktree on branch <branch>:
- <lead> (here): <path>
- <repo>: <path>
```

### Project prompt

```
Make an end-to-end plan for the Linear project "<name>".
<url>

<location>

Cover the whole stack: what changes in each repo, the contracts between them, and the order to ship in. Map each step to the issues below.

## Project
<description>
<content>

## Issues (done ones are already shipped: build on them, don't plan them again)
- <ID> <title> [<state>]
  <description, first 600 characters>     (not for done or cancelled issues)
```

Up to 100 issues. The code marks that ceiling with a `ponytail:` comment, to paginate if a project ever exceeds it.

## Backend

### `linear.go` (new)

A GraphQL client over `net/http`: `POST https://api.linear.app/graphql` with the header `Authorization: <key>`. One helper, `linearQuery(ctx, key, query, vars, out)`, maps HTTP 401 and `AUTHENTICATION_ERROR` to `errLinearAuth`, and `RATELIMITED` to `errLinearRateLimited`. Queries:

- `viewer { name displayName }`
- my work: `viewer.assignedIssues` filtered on `state.type nin [completed, canceled]`, and `projects` filtered on lead or member `isMe` and `status.type nin [completed, canceled]`
- `searchIssues(term:, first: 20)` and `searchProjects(term:, first: 10)`
- `issue(id:)`: `identifier title description url branchName state { name type } team { key } project { name }`
- `project(id:)`: `name description content url status { name type } targetDate progress issues(first: 100) { nodes { identifier title description branchName state { name type } } }`

Field and filter names are from Linear's published schema (`packages/sdk/src/schema.graphql`). The first implementation task confirms the my-work filters with a live query.

### `keychain.go` (new)

`secretGet`, `secretSet` and `secretDelete` for service `grove`, account `linear`.

- macOS: `/usr/bin/security`. Writes go through `security -i` with the command on stdin, so the key never appears in a process listing. Reads use `find-generic-password -w`.
- Linux: `secret-tool store|lookup|clear service grove account linear`, the key on stdin.

The key is read once per run and held in memory in Go. It is never sent to the frontend. `grove doctor` checks for `secret-tool` on Linux.

### `start.go` (new)

- `planWorktree(r Repo, branch, name string) RepoPlan` returns the action and path:
  - **reuse:** the branch is already checked out in a worktree of this repo (that worktree's path, whatever its name), or `<root>/<name>` is already a worktree on this branch
  - **conflict:** `<root>/<name>` exists and is not a worktree on this branch (reason: "<path> already exists"), or the repo can't be read
  - **checkout:** the branch exists locally or as `origin/<branch>`
  - **create:** otherwise, from the repo's `base`
- `startWorktree` carries a plan out with the existing `createWorktree` or `checkoutWorktree`, or returns the reused path.

### `session.go`

`pane` gets two unexported fields, `args string` (appended to the command when the process starts) and `env []string` (appended to `paneEnv`). `sessions.seed(ws, setup, args, env)` opens the workspace like `ensure` and sets them on its first Claude pane, or, when the workspace is already open, adds a new Claude tab carrying them. Seeding happens before the frontend first sizes the pane, so the process always starts seeded.

### Bindings (`linear.go`, `start.go`)

| Method | Returns |
|---|---|
| `LinearAccount()` | `{connected, name}` |
| `LinearConnect(key)` | `{connected, name}`, or `errLinearAuth` |
| `LinearDisconnect()` | |
| `LinearSearch(term)` | `{issues, projects}`; `term == ""` is my work. The frontend marks issues whose branch is checked out, from the snapshot it already has |
| `GetLinearIssue(id)`, `GetLinearProject(id)` | The full issue, or the project with its issues. The frontend suggests the branch and name (`start.ts`) |
| `PlanRepo(repoPath, branch, name)` | `{action, path, reason}`. Also a conflict for a branch name git rejects. The frontend adds a conflict when two ticked repos plan the same folder |
| `StartRepo(repoPath, branch, name, seed)` | The workspace dir. `seed` is `{prompt, addDirs}` for the lead and empty otherwise. Opens the workspace with `setup` queued (not for reuse) |

Menu: **File ▸ Start Work from Linear… ⌘L**. The ⌘/ help lists it.

## Frontend

- `Linear.tsx`: the Connect, search and Start work dialogs, following the existing dialog patterns in `App.tsx` (`overlay`, `dialog form`, `keepFocus`, Y/N and Esc handling).
- `start.ts`: pure logic, tested in `start.test.ts`. Covers the default selection (last-used set intersected with the window's repos, else the current repo), lead fallback, slugs, and building the issue and project prompts. The form's preview and the seed use the same function.
- `App.tsx`: wires the ⌘L menu event and the Settings section, and selects the lead's dir when Start work finishes.

## Errors

| Situation | Behaviour |
|---|---|
| No key saved | ⌘L opens Connect |
| Key rejected (`errLinearAuth`) | Connect opens, saying "Linear rejected the saved key" |
| Search rate-limited | The last results stay, with "Linear is limiting searches, try again in a moment" |
| Network or other API error | Shown in the palette. No automatic retry loop |
| Keychain unavailable | Connect shows the reason. `grove doctor` flags a missing `secret-tool` |
| Conflict in a ticked repo | Shown on its row before anything is created. Create stays disabled |
| A repo fails while creating | git's reason on its row, with Retry or Skip. Created worktrees stay |
| The lead fails | Nothing is seeded until a Retry succeeds |
| No Claude entry in `panes` | The seed runs `claude` |

## Testing

One focused check per piece of logic, in the style of the existing tests.

- `linear_test.go`: the client against an `httptest.Server` with canned responses. Covers search parsing, the identifier shortcut, 401 becoming `errLinearAuth`, and `RATELIMITED` becoming `errLinearRateLimited`.
- `start_test.go`: `planWorktree` on temporary repos, using `worktree_test.go`'s helpers. Covers create, local checkout, remote checkout, reuse by branch, reuse by path, and conflict.
- `session_test.go`: a seeded pane that hasn't started gets the args and env, a seeded open workspace gets a new tab, and a prompt with quotes and newlines survives `sh -c` unchanged.
- `start.test.ts`: default selection, lead fallback, project branch prefix, and the single-repo, multi-repo and project prompts.
- Keychain: a thin wrapper over OS tools, checked by hand and by `grove doctor`.

## Out of scope

- Writing to Linear (status, comments, attachments)
- OAuth, and a key from an environment variable
- The coordinator model (prototype row 4)
- Collapsing Claude Code's `.claude/worktrees/*` in the sidebar: queued as its own task
