# Start Work from Linear Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ⌘L searches Linear, and Start work creates worktrees in the window's repos with the lead repo's Claude Code seeded in plan mode.

**Architecture:** A stdlib GraphQL client (`linear.go`) and an OS-keychain wrapper (`keychain.go`) back a few Wails bindings. `start.go` plans and carries out one repo's worktree using the existing `createWorktree` / `checkoutWorktree`, and seeds the lead through new hidden pane fields in `session.go`. The frontend gets `start.ts` (pure, tested logic) and `Linear.tsx` (Connect, search palette and Start work dialogs).

**Tech Stack:** Go 1.25 (`net/http`, `encoding/json`, `os/exec`), Wails v2, React 19 + TypeScript, `node --test` for frontend tests.

**Spec:** `docs/superpowers/specs/2026-10-22-linear-start-work-design.md`. Prototype: https://claude.ai/artifact/5Z4ysSeNUzPNEEomonZMwv (rows 1 to 3).

## Global Constraints

- No new Go modules or npm packages.
- The Linear key never reaches the frontend, a file, a log or a command line: it goes to the keychain tools on stdin and lives in memory in Go.
- grove only reads from Linear.
- Endpoint `https://api.linear.app/graphql`, header `Authorization: <key>` (no `Bearer`).
- Seed flags exactly `--permission-mode plan`, `--add-dir '<dir>'`; env exactly `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`, only when there is at least one `--add-dir`.
- Error copy exactly: `Linear isn't connected`, `Linear rejected the saved key`, `Linear is limiting searches, try again in a moment`.
- Search waits 400ms after typing stops. An identifier matches `^[A-Za-z]+-\d+$`.
- `localStorage` key for the last-used repos: `grove.linearRepos`.
- UI copy and docs in British English, with no em dashes.
- Shortcuts go through the `shortcuts` table in `app.go`, so `accel()` gives ⌘ on macOS and Ctrl+Shift elsewhere.
- Match the surrounding code: short comments that say why, `ponytail:` comments on deliberate ceilings.
- Regenerate bindings with `~/go/bin/wails generate module` (or `wails` on PATH) after changing bound methods or their types, and commit `frontend/wailsjs`.
- Go tests need `frontend/dist` (the binary embeds it): run `make test`, or `cd frontend && npm run build` once before `go test ./...`.

## Review Focus

1. **Two repos sharing one `worktree_root`:** both plan the same `<root>/<name>`. The form must flag the clash before Create, not fail halfway. (Task 6, `clashes`)
2. **A branch name git rejects, typed into the form** (e.g. `stu/bad..name`): a conflict on the row, not a git error after other repos were created. (Task 3)
3. **The ticket's branch already checked out in the main checkout:** reuse that checkout, rather than `git worktree add` failing. (Task 3)
4. **An issue with no description, or a project with no content and no issues:** the prompt has no empty headings. (Task 6)
5. **Search responses arriving out of order while typing:** an older response must not replace newer results. (Task 6, `latest`, used in Task 7)

---

### Task 1: Linear GraphQL client

**Files:**
- Create: `linear.go`
- Test: `linear_test.go`

**Interfaces:**
- Produces:
  ```go
  type LinearIssue struct {
      ID          string `json:"id"`          // identifier, e.g. "ENG-412"
      Title       string `json:"title"`
      Description string `json:"description"`
      URL         string `json:"url"`
      BranchName  string `json:"branchName"`
      State       string `json:"state"`     // workflow state name, e.g. "In Progress"
      StateType   string `json:"stateType"` // triage|backlog|unstarted|started|completed|canceled
      Team        string `json:"team"`      // team key, e.g. "ENG"
      Project     string `json:"project"`   // project name, "" for none
  }
  type LinearProject struct {
      ID          string        `json:"id"`
      Name        string        `json:"name"`
      Description string        `json:"description"`
      Content     string        `json:"content"`    // filled by linearProject only
      URL         string        `json:"url"`
      Status      string        `json:"status"`     // e.g. "In Progress"
      StatusType  string        `json:"statusType"` // backlog|planned|started|paused|completed|canceled
      TargetDate  string        `json:"targetDate"` // "YYYY-MM-DD" or ""
      Progress    float64       `json:"progress"`   // 0 to 1
      Issues      []LinearIssue `json:"issues"`     // filled by linearProject only
  }
  type LinearResults struct {
      Issues   []LinearIssue   `json:"issues"`
      Projects []LinearProject `json:"projects"`
  }
  var errLinearNoKey       = errors.New("Linear isn't connected")
  var errLinearAuth        = errors.New("Linear rejected the saved key")
  var errLinearRateLimited = errors.New("Linear is limiting searches, try again in a moment")
  var linearEndpoint = "https://api.linear.app/graphql" // a var so tests can point it at httptest
  func linearQuery(ctx context.Context, key, query string, vars map[string]any, out any) error
  func linearViewer(ctx context.Context, key string) (string, error) // the viewer's name
  func linearMyWork(ctx context.Context, key string) (LinearResults, error)
  func linearSearch(ctx context.Context, key, term string) (LinearResults, error)
  func linearIssue(ctx context.Context, key, id string) (LinearIssue, error)
  func linearProject(ctx context.Context, key, id string) (LinearProject, error)
  ```

The GraphQL documents, verified against Linear's published schema (search results are `IssueSearchResult` / `ProjectSearchResult`, not `Issue` / `Project`, so use string constants for the selections rather than fragments):

```go
const issueFields = `identifier title description url branchName state { name type } team { key } project { name }`
const projectFields = `id name description url progress targetDate status { name type }`

qViewer  = `query { viewer { name } }`
qMyWork  = `query {
  viewer { assignedIssues(first: 50, orderBy: updatedAt, filter: { state: { type: { nin: ["completed", "canceled"] } } }) { nodes { ` + issueFields + ` } } }
  projects(first: 20, orderBy: updatedAt, filter: { status: { type: { nin: ["completed", "canceled"] } }, or: [{ lead: { isMe: { eq: true } } }, { members: { some: { isMe: { eq: true } } } }] }) { nodes { ` + projectFields + ` } }
}`
qSearch  = `query($term: String!) {
  searchIssues(term: $term, first: 20) { nodes { ` + issueFields + ` } }
  searchProjects(term: $term, first: 10) { nodes { ` + projectFields + ` } }
}`
qIssue   = `query($id: String!) { issue(id: $id) { ` + issueFields + ` } }`
qProject = `query($id: String!) { project(id: $id) { ` + projectFields + ` content issues(first: 100) { nodes { ` + issueFields + ` } } } }`
```

- [ ] **Step 1: Write the failing tests**

`linear_test.go` needs a helper that stands in for Linear, plus fixtures:

```go
// linearServer stands in for Linear's API: it checks the key and answers each
// request with handle's status and body.
func linearServer(t *testing.T, handle func(query string, vars map[string]any) (int, string))
// It starts an httptest.Server, errors if Authorization != "lin_test", decodes
// {"query","variables"}, writes handle's answer, and swaps linearEndpoint for
// the test (restored in t.Cleanup).

const issueJSON = `{"identifier":"ENG-412","title":"Let people snooze","description":"Pause pushes","url":"https://linear.app/acme/issue/ENG-412","branchName":"stu/eng-412-let-people-snooze","state":{"name":"In Progress","type":"started"},"team":{"key":"ENG"},"project":{"name":"Notification controls"}}`
var issue412 = LinearIssue{ID: "ENG-412", Title: "Let people snooze", Description: "Pause pushes", URL: "https://linear.app/acme/issue/ENG-412", BranchName: "stu/eng-412-let-people-snooze", State: "In Progress", StateType: "started", Team: "ENG", Project: "Notification controls"}
const projectJSON = `{"id":"p1","name":"Notification controls","description":"Finer control","url":"https://linear.app/acme/project/nc","progress":0.43,"targetDate":"2026-11-14","status":{"name":"In Progress","type":"started"}}`
var projectNC = LinearProject{ID: "p1", Name: "Notification controls", Description: "Finer control", URL: "https://linear.app/acme/project/nc", Status: "In Progress", StatusType: "started", TargetDate: "2026-11-14", Progress: 0.43}
```

Tests (compare with `reflect.DeepEqual`):

- `TestLinearSearch`: the handler errors unless the query contains `searchIssues` and `vars["term"] == "snooze"`, and returns `{"data":{"searchIssues":{"nodes":[issueJSON]},"searchProjects":{"nodes":[projectJSON]}}}`. `linearSearch(ctx, "lin_test", " snooze ")` gives `Issues == []LinearIssue{issue412}` and `Projects == []LinearProject{projectNC}`.
- `TestLinearSearchIdentifier`: the handler records every query. With `vars["id"] == "ENG-412"` it returns `{"data":{"issue":issueJSON}}`, otherwise `{"data":null,"errors":[{"message":"Entity not found: Issue","extensions":{"code":"INVALID_INPUT"}}]}`. Then `linearSearch(ctx, "lin_test", "eng-412")` gives `Issues == [issue412]` and no projects. `linearSearch(ctx, "lin_test", "ENG-999")` gives empty results and a nil error. No recorded query contains `searchIssues`.
- `TestLinearMyWork`: `term == ""`. The handler errors unless the query contains `assignedIssues`, and returns `{"data":{"viewer":{"assignedIssues":{"nodes":[<issueJSON with "project":null>]}},"projects":{"nodes":[projectJSON]}}}`. The result has one issue with `Project == ""` and `Projects == [projectNC]`.
- `TestLinearProject`: `linearProject(ctx, "lin_test", "p1")`. The handler checks `vars["id"] == "p1"` and returns the project with `"content":"## Plan"` and `"issues":{"nodes":[issueJSON]}`. The result is `projectNC` plus `Content: "## Plan"` and `Issues: []LinearIssue{issue412}`.
- `TestLinearErrors`, a table through `linearViewer`:

  | status | body | want |
  |---|---|---|
  | 401 | `{"errors":[{"message":"Authentication required"}]}` | `errors.Is(err, errLinearAuth)` |
  | 400 | `{"errors":[{"message":"bad key","extensions":{"code":"AUTHENTICATION_ERROR"}}]}` | `errLinearAuth` |
  | 200 | `{"errors":[{"message":"slow down","extensions":{"code":"RATELIMITED"}}]}` | `errLinearRateLimited` |
  | 429 | `` | `errLinearRateLimited` |
  | 200 | `{"errors":[{"message":"boom"}]}` | error text contains `boom` |
  | 502 | `bad gateway` | error text contains `HTTP 502` |

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npm run build && cd .. && go test -run 'TestLinear' ./...`
Expected: FAIL to compile, `undefined: linearSearch` (and the others)

- [ ] **Step 3: Implement `linear.go`**

- `linearQuery`: POST JSON `{"query","variables"}` with `Content-Type: application/json` and `Authorization: <key>`, using an `http.Client` with a 15s timeout. Decode the body as `{"data": json.RawMessage, "errors": [{"message", "extensions": {"code"}}]}` whatever the status, since Linear sends GraphQL errors with 400. Then, in order:
  - HTTP 401 or code `AUTHENTICATION_ERROR` gives `errLinearAuth`
  - HTTP 429 or code `RATELIMITED` gives `errLinearRateLimited`
  - any other error gives `fmt.Errorf("Linear: %s", message)`
  - any other non-200 gives `fmt.Errorf("Linear: HTTP %d", code)`
  - otherwise unmarshal `data` into `out`
- Decode into small unexported structs that mirror the nested JSON (`state{name,type}`, `team{key}`, `project{name}` which may be null, `status{name,type}`, `issues{nodes}`), and flatten them into the exported types with one `toIssue` and one `toProject` helper.
- `linearSearch`: trim the term. `""` calls `linearMyWork`. An identifier calls `linearIssue(strings.ToUpper(term))`: `errLinearAuth` or `errLinearRateLimited` is returned, any other error gives empty results. Otherwise run `qSearch`.
- `linearProject`: add `// ponytail: first 100 issues; paginate if a project ever has more.`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run 'TestLinear' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w linear.go linear_test.go
git add linear.go linear_test.go
git commit -m "feat: Linear GraphQL client"
```

---

### Task 2: Keychain, Linear bindings, doctor

**Files:**
- Create: `keychain.go`
- Modify: `linear.go` (bindings at the end, under `// ---- bound to the frontend ----`)
- Modify: `app.go` (`App` struct: the cached key)
- Modify: `doctor.go`
- Regenerate: `frontend/wailsjs/go/main/App.{d.ts,js}`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: Task 1's `linearViewer`, `linearSearch`, `linearIssue`, `linearProject` and the `errLinear*` errors.
- Produces:
  ```go
  // keychain.go: service "grove"
  func secretGet(account string) (string, error) // "", nil when there is none
  func secretSet(account, secret string) error
  func secretDelete(account string) error // nil when there is none

  type LinearAccount struct {
      Connected bool   `json:"connected"`
      Name      string `json:"name"`
  }
  func (a *App) LinearAccount() (LinearAccount, error)
  func (a *App) LinearConnect(key string) (LinearAccount, error)
  func (a *App) LinearDisconnect() error
  func (a *App) LinearSearch(term string) (LinearResults, error)
  func (a *App) GetLinearIssue(id string) (LinearIssue, error)
  func (a *App) GetLinearProject(id string) (LinearProject, error)
  ```
  TypeScript, after regenerating: `api.LinearAccount()`, `api.LinearConnect(key)`, `api.LinearDisconnect()`, `api.LinearSearch(term)`, `api.GetLinearIssue(id)` and `api.GetLinearProject(id)`, with types `main.LinearAccount`, `main.LinearResults`, `main.LinearIssue` and `main.LinearProject`.

- [ ] **Step 1: Implement `keychain.go`**

  - **macOS:** read with `/usr/bin/security find-generic-password -s grove -a <account> -w` (exit status 44 means none). Write by running `/usr/bin/security -i` with this on stdin, so the key never appears in argv:
    ```
    add-generic-password -U -s grove -a <account> -w "<secret>"
    ```
    First reject a secret containing whitespace, `"`, `'` or `\` with `errors.New("that doesn't look like a Linear API key")`. Delete with `delete-generic-password -s grove -a <account>`, where status 44 is fine.
  - **Linux:** `secret-tool lookup service grove account <account>`, where a non-zero exit with empty output means none. `secret-tool store --label="grove Linear key" service grove account <account>` with the secret on stdin. `secret-tool clear service grove account <account>`. If `secret-tool` is missing, return `errors.New("secret-tool not found: install libsecret-tools to connect Linear")`.
  - **Anything else:** `fmt.Errorf("no keychain support on %s", runtime.GOOS)`.

- [ ] **Step 2: Implement the bindings in `linear.go`**

  - Add `linKey, linName string // Linear API key from the keychain, and the account's name; guarded by mu` to `App`.
  - `func (a *App) linearKey() (string, error)` returns the cached key, else `secretGet("linear")` and caches it. An empty key gives `errLinearNoKey`.
  - `LinearAccount`:
    - no key: `{Connected: false}, nil`
    - a keychain error: return the error
    - name not cached: call `linearViewer`. `errLinearAuth` gives `{Connected: false}, errLinearAuth`. Any other error (offline) gives `{Connected: true}, nil`, so ⌘L still opens the palette, which then shows the error.
  - `LinearConnect`: `strings.TrimSpace` the key, then `linearViewer`. On success, `secretSet("linear", key)`, cache the key and name, and return `{true, name}`.
  - `LinearDisconnect`: `secretDelete("linear")`, then clear the cache.
  - `LinearSearch`, `GetLinearIssue` and `GetLinearProject` call `linearKey()` and then the client, with `context.Background()`.

- [ ] **Step 3: Add the keychain check to `doctor.go`**

  - **Linux:** `exec.LookPath("secret-tool")`. If found, `check("keychain", path, true)`. Otherwise print `[opt ] keychain     secret-tool not found: install libsecret-tools to connect Linear`, in the format the existing `terminal` line uses.
  - **macOS:** `check("keychain", "/usr/bin/security", fileExists("/usr/bin/security"))`.

- [ ] **Step 4: Regenerate the bindings, then build and vet**

Run: `~/go/bin/wails generate module && cd frontend && npm run build && cd .. && go vet ./... && go test ./...`
Expected: `frontend/wailsjs/go/main/App.d.ts` declares `LinearSearch(arg1:string):Promise<main.LinearResults>` and the other five. vet is clean and all tests pass.

- [ ] **Step 5: Check doctor**

Run: `go run . doctor`
Expected: a `keychain` line (`[OK  ] keychain     /usr/bin/security` on macOS)

- [ ] **Step 6: Commit**

```bash
gofmt -w keychain.go linear.go app.go doctor.go
git add keychain.go linear.go app.go doctor.go frontend/wailsjs
git commit -m "feat: Linear key in the keychain and Linear bindings"
```

---

### Task 3: Plan and start one repo's worktree

**Files:**
- Create: `start.go`
- Test: `start_test.go`

**Interfaces:**
- Consumes: `resolveWorktreeRoot`, `worktreesOf`, `branchExists`, `isRemote`, `createWorktree`, `checkoutWorktree`, `workspaceFor`, `realPath`, `tildePath` (all existing).
- Produces:
  ```go
  type RepoPlan struct {
      Action string `json:"action"` // "create" | "checkout" | "reuse" | "conflict"
      Path   string `json:"path"`   // the worktree's dir ("" for some conflicts)
      Reason string `json:"reason"` // a conflict's one line
  }
  func planWorktree(r Repo, branch, name string) RepoPlan
  func startWorktree(r Repo, branch, name string) (Workspace, RepoPlan, error)
  ```

- [ ] **Step 1: Write the failing tests**

```go
// startRepo makes a repo and a clone of it, so the clone has an origin, and
// returns the clone as a Repo whose new worktrees go under root.
func startRepo(t *testing.T) (origin string, r Repo, root string) {
	t.Helper()
	origin = gitRepo(t)
	clone := filepath.Join(filepath.Dir(origin), "clone")
	gitT(t, "", "clone", "-q", origin, clone)
	root = filepath.Join(filepath.Dir(origin), "wt")
	return origin, Repo{Path: clone, WorktreeRoot: root, Base: "origin/trunk"}, root
}

func TestPlanWorktree(t *testing.T) {
	origin, r, root := startRepo(t)
	gitT(t, r.Path, "branch", "stu/local")
	gitT(t, origin, "branch", "stu/remote")
	gitT(t, r.Path, "fetch", "-q", "origin")
	other := filepath.Join(filepath.Dir(origin), "elsewhere")
	gitT(t, r.Path, "worktree", "add", "-q", "-b", "stu/reuse", other)
	if err := os.MkdirAll(filepath.Join(root, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := func(p string) string { p, _ = filepath.EvalSymlinks(p); return p }
	for _, c := range []struct{ branch, name, action, path, reason string }{
		{"stu/new", "eng-1", "create", filepath.Join(root, "eng-1"), ""},
		{"stu/local", "local", "checkout", filepath.Join(root, "local"), ""},
		{"stu/remote", "remote", "checkout", filepath.Join(root, "remote"), ""},
		{"stu/reuse", "anything", "reuse", real(other), ""},
		{"trunk", "main", "reuse", real(r.Path), ""}, // the main checkout is on it
		{"stu/new", "taken", "conflict", "", "already exists"},
		{"stu/new", "a/b", "conflict", "", "path separator"},
		{"stu/bad..name", "bad", "conflict", "", "not a valid branch name"},
	} {
		p := planWorktree(r, c.branch, c.name)
		if p.Action != c.action || (c.path != "" && p.Path != c.path) || !strings.Contains(p.Reason, c.reason) {
			t.Errorf("planWorktree(%q, %q) = %+v, want %s %s %q", c.branch, c.name, p, c.action, c.path, c.reason)
		}
	}
}
```

- `TestStartWorktreeFetchesPushedBranch`: on `origin`, `checkout -q -b stu/pushed`, commit a `b.txt`, then `checkout -q trunk`. The clone hasn't fetched since. `startWorktree(r, "stu/pushed", "pushed")` gives no error, `plan.Action == "checkout"`, `ws.Branch == "stu/pushed"`, and `b.txt` exists in `ws.Dir`.
- `TestStartWorktreeCreateReuseConflict`:
  - `startWorktree(r, "stu/eng-2", "eng-2")` gives `"create"` and `ws.Dir == realPath(filepath.Join(root, "eng-2"))`.
  - The same call again gives `"reuse"` with the same `ws.Dir`.
  - `startWorktree(r, "stu/eng-3", "eng-2")` gives an error containing `already exists`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestPlanWorktree|TestStartWorktree' ./...`
Expected: FAIL to compile, `undefined: planWorktree`

- [ ] **Step 3: Implement `planWorktree` in `start.go`**

Check these in order:
1. Empty `name`, or one containing `/` or `\`, gives a conflict: `worktree name must not be empty or contain a path separator`.
2. Empty `branch`, or `git check-ref-format --branch <branch>` failing, gives a conflict: `<branch> is not a valid branch name`.
3. If `worktreesOf(repoPath)` fails, the conflict is `can't read <tildePath(repoPath)>: <err>`.
4. A worktree whose `Branch == branch` gives **reuse** with that worktree's `Path`.
5. If `filepath.Join(root, name)` exists, the conflict is `<tildePath(path)> already exists`.
6. If `branchExists` is true, or `git show-ref --verify --quiet refs/remotes/origin/<branch>` succeeds, the plan is **checkout**.
7. Otherwise **create**.

- [ ] **Step 4: Implement `startWorktree`**

If the plan is `create` and the repo has an `origin`, run `git -C <repo> fetch -q origin <branch>` and ignore its error. That finds a branch someone already pushed. Then plan again and act on the plan:
- conflict: `errors.New(plan.Reason)`
- reuse: `workspaceFor(r, plan.Path, branch)`
- checkout: `checkoutWorktree(r, branch, name)` if the local branch exists, else `checkoutWorktree(r, "origin/"+branch, name)`
- create: `createWorktree(r, name, branch, "")`

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -run 'TestPlanWorktree|TestStartWorktree' ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
gofmt -w start.go start_test.go
git add start.go start_test.go
git commit -m "feat: plan and start a ticket's worktree in one repo"
```

---

### Task 4: Seed a Claude pane

**Files:**
- Modify: `session.go` (the `pane` struct, `start`, new `seed`, `claudeCmd`, `seedArgs`, `shellQuote`)
- Modify: `app.go:533-550` (`NewTab` uses `claudeCmd`)
- Test: `session_test.go`

**Interfaces:**
- Produces:
  ```go
  // on pane: args string (appended to Cmd when the process starts); env []string (appended to paneEnv)
  func (s *sessions) seed(ws Workspace, setup, args string, env []string) []Pane
  func claudeCmd(ws Workspace) string             // first Claude entry in ws.Panes, else "claude"
  func seedArgs(prompt string, addDirs []string) string
  func shellQuote(s string) string                // POSIX single quotes: ' becomes '\''
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestSeedArgs(t *testing.T) {
	if got, want := seedArgs("p", nil), `--permission-mode plan 'p'`; got != want {
		t.Errorf("seedArgs = %s, want %s", got, want)
	}
	if got, want := seedArgs("it's", []string{"/a", "/b c"}), `--permission-mode plan --add-dir '/a' --add-dir '/b c' 'it'\''s'`; got != want {
		t.Errorf("seedArgs = %s, want %s", got, want)
	}
}
```

- `TestSeedPanes` uses `s := newSessions("", func(string, ...any) {})`, so no process starts, and reads `s.panes[id]` directly:
  - Seeding an unopened `ws{Panes: ["claude --model opus", ""]}` with setup `make setup`, args `--x` and env `[SEEDED=1]` gives 2 panes. `ps[0].Cmd == "claude --model opus"` (the visible command is unchanged) and its `Kind == "claude"`. Pane 0 has `args == "--x"` and `env == [SEEDED=1]`. Pane 1 has `setup == "make setup"` and empty `args`.
  - Seeding the same, now open, workspace with `--y` gives 3 panes. The new one has `Cmd == "claude --model opus"` and `args == "--y"`.
  - Seeding an unopened `ws{Panes: [""]}` with `--z` gives 2 panes, with `ps[1].Cmd == "claude"` and `args == "--z"`.
- `TestSeedReachesClaudeVerbatim`:
  - Setup: `t.Setenv("SHELL", "/bin/sh")`. Write an executable fake `claude` into a temp dir prepended to `PATH`: `#!/bin/sh` then `printf '[%s]' "$@"; printf 'env=%s;' "$SEEDED"` (`os.Chmod` it to 0o755).
  - With this prompt (quotes, `$HOME`, backticks and a newline), seed `Workspace{Panes: ["claude"]}` with `seedArgs(prompt, []string{"/a b"})` and env `[SEEDED=1]`, then `s.start(id, 200, 24)`:
    ```go
    prompt := "it's \"quoted\" $HOME \x60id\x60\nline 2"
    ```
  - Use `waitFor` until the recorder's output, with `\r\n` replaced by `\n`, contains `[--permission-mode][plan][--add-dir][/a b][` + prompt + `]env=1;`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestSeed' ./...`
Expected: FAIL to compile, `undefined: seedArgs`

- [ ] **Step 3: Implement the changes**

- `start()` runs `paneCommand(p.Cmd + " " + p.args)` when `args != ""`, and sets `c.Env = append(paneEnv(...), p.env...)`.
- `seed`:
  - **Not open:** do what `ensure` does, under the lock. Put the args and env on the first pane whose `Kind == "claude"`. If there is none, append a `claudeCmd(ws)` pane carrying them.
  - **Open:** append a `claudeCmd(ws)` pane carrying them, as `newTab` does.
  - Emit `changed`, and return the workspace's panes.
- `NewTab`'s `"claude"` case calls `claudeCmd(ws)` instead of its inline search.

- [ ] **Step 4: Run all Go tests**

Run: `go test ./...`
Expected: PASS (including the existing `TestTabsOpenAndCloseWorkspace`)

- [ ] **Step 5: Commit**

```bash
gofmt -w session.go session_test.go app.go
git add session.go session_test.go app.go
git commit -m "feat: seed a Claude pane with flags and env before it starts"
```

---

### Task 5: Start-work bindings and the ⌘L menu item

**Files:**
- Modify: `start.go` (bindings)
- Modify: `app.go` (`shortcuts` table)
- Test: `start_test.go`
- Regenerate: `frontend/wailsjs`

**Interfaces:**
- Consumes: Task 3's `planWorktree` and `startWorktree`, Task 4's `sessions.seed` and `seedArgs`, and the existing `App.repo`, `App.reload` and `sessions.ensure`.
- Produces:
  ```go
  type Seed struct {
      Prompt  string   `json:"prompt"`  // "" = not the lead: no seed
      AddDirs []string `json:"addDirs"`
  }
  func (a *App) PlanRepo(repoPath, branch, name string) (RepoPlan, error)
  func (a *App) StartRepo(repoPath, branch, name string, seed Seed) (string, error) // the workspace dir
  ```
  The menu action is `"linear"`, with the label `Start Work from Linear…` and the key `l` with `cmdMod`. TypeScript gets `api.PlanRepo(repo, branch, name)`, `api.StartRepo(repo, branch, name, seed)`, `main.RepoPlan` and `main.Seed`.

- [ ] **Step 1: Write the failing tests**

```go
func TestStartRepo(t *testing.T) {
	_, r, root := startRepo(t)
	r.Panes, r.Setup = []string{"claude", ""}, "make setup"
	a := newTestApp(&Config{Repo: []Repo{r}})

	// The lead: seeded, with the other repos' instructions loaded.
	dir, err := a.StartRepo(r.Path, "stu/lead", "lead", Seed{Prompt: "plan it", AddDirs: []string{"/other"}})
	if err != nil || dir != realPath(filepath.Join(root, "lead")) {
		t.Fatalf("StartRepo = %q, %v", dir, err)
	}
	ps := a.sess.open[dir]
	if ps[0].args != seedArgs("plan it", []string{"/other"}) || !slices.Contains(ps[0].env, "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1") || ps[1].setup != "make setup" {
		t.Errorf("lead panes: args %q env %q setup %q", ps[0].args, ps[0].env, ps[1].setup)
	}
	// Another repo: no seed, setup queued.
	dir, _ = a.StartRepo(r.Path, "stu/other", "other", Seed{})
	if ps := a.sess.open[dir]; ps[0].args != "" || ps[0].env != nil || ps[1].setup != "make setup" {
		t.Errorf("non-lead panes: %+v %+v", ps[0], ps[1])
	}
	// A lead alone: nothing to add, so no env.
	dir, _ = a.StartRepo(r.Path, "stu/solo", "solo", Seed{Prompt: "p"})
	if a.sess.open[dir][0].env != nil {
		t.Errorf("solo lead env = %q", a.sess.open[dir][0].env)
	}
	if p, err := a.PlanRepo(r.Path, "stu/lead", "x"); err != nil || p.Action != "reuse" {
		t.Errorf("PlanRepo = %+v, %v", p, err)
	}
	if !slices.ContainsFunc(shortcutInfos(), func(s ShortcutInfo) bool { return s.Label == "Start Work from Linear" }) {
		t.Error("no Start Work from Linear shortcut")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestStartRepo ./...`
Expected: FAIL to compile, `a.StartRepo undefined`

- [ ] **Step 3: Implement**

- `PlanRepo` calls `a.repo(repoPath)`, returning its error, then `planWorktree`.
- `StartRepo`:
  1. Call `a.repo`, then `startWorktree`, then `a.reload()`.
  2. Set `setup := r.Setup`, or `""` when the plan was `reuse`.
  3. With no `seed.Prompt`, call `a.sess.ensure(ws, setup)`.
  4. Otherwise call `a.sess.seed(ws, setup, seedArgs(seed.Prompt, seed.AddDirs), env)`, where `env` is `[]string{"CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1"}` only when `len(seed.AddDirs) > 0`.
  5. Return `ws.Dir`.
- Add `{"File", "Start Work from Linear…", "linear", "l", cmdMod},` to `shortcuts` straight after `New Worktree from Branch…`.

- [ ] **Step 4: Regenerate the bindings and run the tests**

Run: `~/go/bin/wails generate module && make test`
Expected: PASS. `App.d.ts` declares `PlanRepo` and `StartRepo`.

- [ ] **Step 5: Commit**

```bash
gofmt -w start.go start_test.go app.go
git add start.go start_test.go app.go frontend/wailsjs
git commit -m "feat: PlanRepo and StartRepo bindings, ⌘L menu item"
```

---

### Task 6: Start-work logic in `start.ts`

**Files:**
- Create: `frontend/src/start.ts`
- Test: `frontend/src/start.test.ts`

**Interfaces:**
- Consumes: `main.LinearIssue`, `main.LinearProject`, `main.RepoInfo` and `main.WorkspaceInfo` from `../wailsjs/go/models`.
- Produces:
  ```ts
  export const LINEAR_NOT_CONNECTED = "Linear isn't connected"
  export const LINEAR_REJECTED = 'Linear rejected the saved key'
  export const LINEAR_LIMITED = 'Linear is limiting searches, try again in a moment'
  export const REPOS_KEY = 'grove.linearRepos'
  export type Ticket = { kind: 'issue'; issue: main.LinearIssue } | { kind: 'project'; project: main.LinearProject }
  export type Target = { repo: string; path: string } // a repo's display name and its worktree dir
  export function defaultRepos(saved: string[], repos: main.RepoInfo[], current: string): string[] // repo paths, window order
  export function nextLead(lead: string, ticked: string[]): string
  export function slug(s: string): string
  export function suggestBranch(t: Ticket): string
  export function suggestName(t: Ticket): string
  export function buildPrompt(t: Ticket, targets: Target[], branch: string): string // targets[0] is the lead
  export function clashes(targets: Target[]): Record<string, string> // repo -> "same folder as <other>"
  export function existingWorktree(branch: string, workspaces: main.WorkspaceInfo[]): main.WorkspaceInfo | undefined
  export function latest(): { next(): number; current(id: number): boolean }
  ```

- [ ] **Step 1: Write the failing tests**

Use the fixtures and `node:test` style of `repos.test.ts`, importing `./start.ts`:

```ts
const issue = { id: 'ENG-412', title: 'Let people snooze', description: 'Pause pushes for a while.', url: 'https://linear.app/acme/issue/ENG-412', branchName: 'stu/eng-412-let-people-snooze', state: 'In Progress', stateType: 'started', team: 'ENG', project: 'Notification controls' } as main.LinearIssue
const project = {
  id: 'p1', name: 'Notification controls', description: 'Finer control.', content: 'Snooze and quiet hours.', url: 'https://linear.app/acme/project/nc',
  status: 'In Progress', statusType: 'started', targetDate: '2026-11-14', progress: 0.43,
  issues: [
    issue,
    { ...issue, id: 'ENG-401', title: 'Store preferences', description: 'Old work', state: 'Done', stateType: 'completed', branchName: 'stu/eng-401-store' },
    { ...issue, id: 'ENG-420', title: 'Profile API', description: 'x'.repeat(700), state: 'Todo', stateType: 'unstarted' },
  ],
} as main.LinearProject
const api = { repo: 'api', path: '/w/api/eng-412' }, ios = { repo: 'ios', path: '/w/ios/eng-412' }
```

Assertions:

- `buildPrompt({ kind: 'issue', issue }, [api], issue.branchName)` equals:
  ```
  Plan the work for Linear issue ENG-412: Let people snooze
  https://linear.app/acme/issue/ENG-412

  You're in the api worktree on branch stu/eng-412-let-people-snooze.

  ## Description
  Pause pushes for a while.
  ```
- `buildPrompt({ kind: 'issue', issue }, [api, ios], issue.branchName)` equals:
  ```
  Plan the work for Linear issue ENG-412: Let people snooze
  https://linear.app/acme/issue/ENG-412

  You're in the api worktree. The work spans 2 repos, each with a worktree on branch stu/eng-412-let-people-snooze:
  - api (here): /w/api/eng-412
  - ios: /w/ios/eng-412

  Plan across every repo before changing anything.

  ## Description
  Pause pushes for a while.
  ```
- With `description: ''`, the single-repo issue prompt ends with `on branch stu/eng-412-let-people-snooze.` (no empty heading).
- `buildPrompt({ kind: 'project', project }, [api, ios], 'stu/notification-controls')` equals:
  ```
  Make an end-to-end plan for the Linear project "Notification controls".
  https://linear.app/acme/project/nc

  You're in the api worktree. The work spans 2 repos, each with a worktree on branch stu/notification-controls:
  - api (here): /w/api/eng-412
  - ios: /w/ios/eng-412

  Cover the whole stack: what changes in each repo, the contracts between them, and the order to ship in. Map each step to the issues below.

  ## Project
  Finer control.

  Snooze and quiet hours.

  ## Issues (done ones are already shipped: build on them, don't plan them again)
  - ENG-412 Let people snooze [In Progress]
    Pause pushes for a while.
  - ENG-401 Store preferences [Done]
  - ENG-420 Profile API [Todo]
    <600 x>…
  ```
  `<600 x>…` is `'x'.repeat(600) + '…'`. Each line of an issue description is indented two spaces. Done and cancelled issues (`stateType` `completed` or `canceled`) get no description.
- A project with `description: ''`, `content: ''` and `issues: []` ends with `Map each step to the issues below.` (no `## Project` or `## Issues`).
- Slugs and suggestions:
  - `slug('Notification controls!') === 'notification-controls'`, `slug('Café ☕ launch') === 'caf-launch'`, `slug('☕') === 'work'`, and `slug('a'.repeat(80)).length === 60`.
  - `suggestBranch` gives the issue's `branchName`. For a project: `'stu/notification-controls'`, and `'notification-controls'` when `issues: []`.
  - `suggestName` gives `'eng-412'` and `'notification-controls'`.
- Defaults and the lead, with repos `/a`, `/b`, `/c`:
  - `defaultRepos(['/c', '/gone', '/a'], repos, '/b')` gives `['/a', '/c']`, `defaultRepos([], repos, '/b')` gives `['/b']`, and `defaultRepos(['/gone'], repos, '')` gives `['/a']`.
  - `nextLead('/b', ['/a', '/b'])` gives `'/b'`, `nextLead('/b', ['/a'])` gives `'/a'`, and `nextLead('/b', [])` gives `''`.
- `clashes([{ repo: 'api', path: '/w/x' }, { repo: 'ios', path: '/w/x' }, { repo: 'web', path: '/w/y' }, { repo: 'and', path: '' }])` deep-equals `{ api: 'same folder as ios', ios: 'same folder as api' }`.
- `existingWorktree('stu/eng-412-let-people-snooze', ws)` finds the workspace on that branch, and `existingWorktree('', ws)` is `undefined`, even when a detached workspace has `branch: ''`.
- With `const s = latest(); const a = s.next(); const b = s.next()`, `s.current(a) === false` and `s.current(b) === true`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npm test`
Expected: FAIL, `Cannot find module './start.ts'`

- [ ] **Step 3: Implement `start.ts`**

- `slug`: lowercase, replace each run of non-`[a-z0-9]` characters with `-`, trim `-`, cut to 60 characters and trim `-` again, falling back to `'work'`.
- `suggestBranch` for a project takes the part before the first `/` of the first issue `branchName` containing `/`.
- `buildPrompt` joins its sections with blank lines and leaves out any that are empty.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd frontend && npm test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/start.ts frontend/src/start.test.ts
git commit -m "feat: start-work logic: defaults, slugs, prompts"
```

---

### Task 7: Connect dialog, Linear settings row and the search palette

**Files:**
- Create: `frontend/src/Linear.tsx`
- Modify: `frontend/src/util.ts` (move `keepFocus` here from `App.tsx`, exported)
- Modify: `frontend/src/App.tsx` (`Modal` union, `dispatch` case `'linear'`, rendering, `SettingsForm` Linear section)
- Modify: `frontend/src/style.css`

**Interfaces:**
- Consumes: Task 2's `api.LinearAccount`, `LinearConnect`, `LinearDisconnect`, `LinearSearch`, `GetLinearIssue`, `GetLinearProject` and `OpenURL` (existing). Task 6's `Ticket`, `existingWorktree`, `latest` and the `LINEAR_*` constants.
- Produces:
  ```ts
  export function ConnectDialog(props: { rejected: boolean; onDone: () => void; onCancel: () => void }): JSX.Element
  export function LinearAccountRow(props: { onConnect: () => void }): JSX.Element
  export function SearchPalette(props: { snap: main.Snapshot; onPick: (t: Ticket) => void; onGoTo: (dir: string) => void; onNeedConnect: (rejected: boolean) => void; onClose: () => void }): JSX.Element
  ```
  New `Modal` members in `App.tsx`: `{ kind: 'linear-connect'; rejected: boolean } | { kind: 'linear-search' } | { kind: 'linear-start'; ticket: Ticket }`. Task 8 renders `'linear-start'`.

The layouts follow the prototype's Connect and Search artboards, using grove's existing `.overlay`, `.dialog`, `.form`, `.buttons` and `kbd` styles.

- [ ] **Step 1: Move `keepFocus` to `util.ts`**, export it and import it in `App.tsx`. Run `cd frontend && npm run build`. Expected: passes.

- [ ] **Step 2: Build `ConnectDialog`**
  - The copy explains where to make a key (**Linear ▸ Settings ▸ Account ▸ Security & access**, a key with **Read** access, with a link that calls `api.OpenURL('https://linear.app/settings/account/security')`), and says it's kept in the keychain, never in `.grove.toml`.
  - It has a `type="password"` field and Cancel/Connect buttons with `<kbd>`.
  - With `rejected`, it shows `Linear rejected the saved key. Paste a new one.` in `.err`.
  - Connect calls `api.LinearConnect(key)`. `LINEAR_REJECTED` shows `Linear didn't accept that key.`, any other error shows `errText(e)`, and success calls `onDone()`.

- [ ] **Step 3: Build `LinearAccountRow` and add it to `SettingsForm`** under an `<h3>Linear</h3>`.
  - On mount it calls `api.LinearAccount()`. Connected shows `Connected as <name>` with a Disconnect button. Otherwise it shows `Not connected`, or the rejected message on `LINEAR_REJECTED`, with a `Connect…` button calling `onConnect`.
  - `SettingsForm` gains an `onLinearConnect` prop, which `App` sets to open `{ kind: 'linear-connect', rejected: false }`.

- [ ] **Step 4: Build `SearchPalette`**
  - **State:** `query`, `scope` (`'all' | 'issues' | 'projects'`), `results`, `cursor`, `err`, and `note` (for rate limiting).
  - **Fetching:** an effect on `query` calls `api.LinearSearch(query.trim())`, straight away when it's empty and after 400ms otherwise. A `latest()` id is checked before applying, and errors are handled:
    - `LINEAR_NOT_CONNECTED` or `LINEAR_REJECTED`: `onNeedConnect(e === LINEAR_REJECTED)`
    - `LINEAR_LIMITED`: keep the results and set `note`
    - anything else: set `err`
  - **Sections:** with an empty query, "Assigned to you" and "Your projects". Otherwise "Issues" and "Projects", filtered by `scope`.
  - **Rows:**
    - An issue row shows a state glyph (inline SVG by `stateType`: `started` half-filled `--waiting`, `unstarted` a ring in `--muted`, `backlog` and `triage` a dashed ring, `completed` filled `--idle`, `canceled` a ring with a slash in `--dim`), the identifier in monospace, the title, `worktree in <repo>` in `--active` when `existingWorktree(issue.branchName, snap.workspaces)` finds one (else the project, else the team), and the state name.
    - A project row shows its name, `<round(progress*100)>% done`, and the target date if it has one, then its status.
  - **Keys** on the input (no global handlers):
    - ↑ and ↓ move the cursor
    - ↩ on an issue with a worktree calls `onGoTo(dir)`
    - ↩ otherwise fetches `api.GetLinearIssue(id)` or `api.GetLinearProject(id)` and calls `onPick(ticket)`, with errors handled as above
    - ⌘↩ (`metaKey || ctrlKey`) calls `api.OpenURL(url)`
    - Esc calls `onClose`
  - **Mouse:** hovering a row moves the cursor, and clicking acts like ↩.
  - **Footer:** `↑↓ select · ↩ start work · ⌘↩ open in Linear · esc close`, where ↩'s label becomes `go to worktree` for such a row.

- [ ] **Step 5: Wire it up in `App.tsx`**
  - The `dispatch` case `'linear'` calls `api.LinearAccount()`:
    - resolves with `connected`: open `linear-search`
    - resolves with `!connected`: open `{ kind: 'linear-connect', rejected: false }`
    - rejects with `LINEAR_REJECTED`: open `{ kind: 'linear-connect', rejected: true }`
    - rejects with anything else (a keychain error): `say(errText(e), true)`
  - Connect's `onDone` opens `linear-search`.
  - The palette's `onGoTo(dir)` closes it and calls `openWs(dir)`. `onPick(ticket)` opens `{ kind: 'linear-start', ticket }`. `onNeedConnect(r)` opens `{ kind: 'linear-connect', rejected: r }`.

- [ ] **Step 6: Add the styles to `style.css`**: `.palette` (`width: min(680px, 94vw)`, no padding, column), `.palette .q` (borderless 15px input), `.chips button[aria-pressed='true']` (accent background), `.hits` (`max-height: 440px; overflow-y: auto`), `.hit` (a flex row; `.hit.cursor` uses the same outline as `.list li.cursor`), `.hit .id`, `.hit .meta` and `.palette footer`.

- [ ] **Step 7: Build and test**

Run: `cd frontend && npm run build && npm test && cd .. && make test`
Expected: tsc is clean and all tests pass.

- [ ] **Step 8: Manual check (needs a real Linear key, so ask your human partner)**

Run `make dev`, then:
1. ⌘L shows Connect, and a bad key shows `Linear didn't accept that key.`
2. A good key opens the palette, listing your assigned issues and your projects. This is the live check of the my-work filters.
3. Typing `snooze`, or any word, shows results after a short pause, and `ENG-123`-style text finds that issue.
4. An issue whose branch is checked out shows `worktree in <repo>`, and ↩ goes to it.
5. ⌘↩ opens the issue in the browser.
6. Settings shows `Connected as <name>`, and Disconnect followed by ⌘L shows Connect.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/Linear.tsx frontend/src/util.ts frontend/src/App.tsx frontend/src/style.css
git commit -m "feat: connect Linear and search issues and projects with ⌘L"
```

---

### Task 8: Start work form, creating and the README

**Files:**
- Modify: `frontend/src/Linear.tsx` (add `StartWork`)
- Modify: `frontend/src/App.tsx` (render `'linear-start'`)
- Modify: `frontend/src/style.css`
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 5's `api.PlanRepo` and `api.StartRepo` and `main.RepoPlan`. Task 6's `defaultRepos`, `nextLead`, `suggestBranch`, `suggestName`, `buildPrompt`, `clashes` and `REPOS_KEY`.
- Produces:
  ```ts
  export function StartWork(props: { snap: main.Snapshot; ticket: Ticket; current: string; onDone: (leadDir: string, created: number) => void; onCancel: () => void }): JSX.Element
  ```
  `current` is the selected worktree's `repoPath`, or `''`.

The layouts follow the prototype's Start work and Creating artboards.

- [ ] **Step 1: Build the form phase of `StartWork`**
  - **Initial state:**
    - `ticked = defaultRepos(<REPOS_KEY from localStorage, parsed safely like savedCollapsed>, snap.repos, current)`
    - `lead = nextLead(current, ticked)`
    - `branch = suggestBranch(ticket)`, `name = suggestName(ticket)`
    - `seed = true`, `showPrompt = false`
  - **Plans:** an effect on `[ticked, branch, name]` waits 250ms, then runs `Promise.all(ticked.map((p) => api.PlanRepo(p, branch, name)))` into `plans: Record<string, main.RepoPlan>`.
  - **Ticket card:**
    - an issue shows its identifier, state, project, title and the description cut to 2 lines
    - a project shows its status, `% done` and target date, then every issue with its state, done ones dimmed
    - both have `Open in Linear ↗`
  - **Repo rows** (`snap.repos`): a checkbox, the name, `<base or plan action> → <plan.path>`, and the lead radio (disabled when unticked). A conflict's `plan.reason`, or `clashes(...)[name]`, shows in `.err` under the row. Unticking the lead calls `nextLead`.
  - **Fields:** Branch (hint `from Linear`, or `suggested from the project name`) and Worktree name.
  - **Seed:** the checkbox reads `Start Claude in plan mode in <lead name>`, adding ` with the other <n> worktree(s) added` when there are several. `▸ Show the prompt` reveals a `<pre>` with `buildPrompt(ticket, targets, branch)`, where `targets` is the lead first and then the other ticked repos, as `{ repo: name, path: plans[p]?.path ?? '' }`.
  - **Create:** labelled `Create worktree`, or `Create <n> worktrees`. It's disabled while nothing is ticked, plans are loading, or any ticked repo has a conflict or a clash.

- [ ] **Step 2: Build the creating phase**
  - On Create, save `ticked` to `REPOS_KEY` and switch to rows of `{ state: 'running' | 'done' | 'failed' | 'skipped', dir?, err? }`.
  - Start every non-lead with `api.StartRepo(p, branch, name, { prompt: '', addDirs: [] })` in parallel.
  - Once every non-lead is `done` or `skipped`, start the lead. With `seed` on, its seed is `{ prompt: buildPrompt(ticket, [lead, ...done non-leads as Targets using their returned dirs], branch), addDirs: <done non-leads' dirs> }`. With it off, the seed is empty.
  - A failed row shows the error with **Retry** (runs that repo again, and the lead step if it was the lead) and, for non-leads, **Skip**.
  - When the lead finishes, call `onDone(leadDir, <count of done rows>)`.
  - Close (Esc) calls `onCancel`, and created worktrees stay.

- [ ] **Step 3: Render it in `App.tsx`**
  - `{ kind: 'linear-start' }` renders `StartWork`, with `current = target()?.repoPath ?? ''`.
  - `onDone(dir, n)` calls `setModal(null)`, then `say(n > 1 ? \`started work in ${n} repos\` : 'started work')`, then `openWs(dir)`.
  - `onCancel` calls `setModal(null)`.

- [ ] **Step 4: Update `README.md`**
  - Add `| Start work from Linear | ⌘L |` to the Worktrees column of the shortcuts table.
  - Add a `## Linear` section after "Claude Code status", in the README's voice:
    - connecting: a personal API key with Read access, kept in the macOS Keychain or, on Linux, through `secret-tool`
    - what the palette lists
    - what Start work does: worktrees in the ticked repos using each repo's `worktree_root`, `base` and `setup`; the lead's Claude in plan mode with `--add-dir` and `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`; projects asking for an end-to-end plan
    - that grove never writes to Linear

- [ ] **Step 5: Build and test**

Run: `cd frontend && npm run build && npm test && cd .. && make test`
Expected: tsc is clean and all tests pass.

- [ ] **Step 6: Manual check (needs a real Linear key and two repos in the window, so ask your human partner)**
1. ⌘L, pick an issue: the branch is Linear's, the name is the identifier, and the current repo is ticked.
2. Tick a second repo and make it the lead: the prompt preview follows.
3. Set the name to one that already exists in one repo: that row shows `… already exists` and Create is disabled.
4. Create: both worktrees appear, `setup` runs in each shell pane, and the lead's Claude starts in plan mode with the prompt (`/context` in it lists the other repo's `CLAUDE.md`).
5. Pick a project: the prompt lists all its issues, done ones without descriptions, and the branch is `<you>/<project-slug>`.
6. Start an issue whose branch exists only on the remote: it's checked out, not created afresh.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/Linear.tsx frontend/src/App.tsx frontend/src/style.css README.md
git commit -m "feat: start work on a Linear issue or project across repos"
```
