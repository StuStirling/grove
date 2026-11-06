# Projects in the sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group worktrees that belong to one piece of work across repos into project groups at the top of the sidebar, derived from folder names with manual overrides, and tag each repo's main checkout.

**Architecture:** Frontend only. A pure `projects.ts` works out membership (dir → project name) from the snapshot and `localStorage` overrides. `repos.ts` `groups()` puts project groups first and drops their rows from the repo groups. `App.tsx` holds the overrides and the ⌘1–9 order; `Sidebar.tsx` renders headers, tags and the row-menu actions.

**Tech Stack:** React 19, TypeScript, Wails bindings (`main.WorkspaceInfo`, `main.RepoInfo`), tests with `node:test` run by `npm test` in `frontend/`.

**Spec:** `docs/superpowers/specs/2026-11-06-sidebar-projects-design.md`

## Global Constraints

- No Go or binding changes.
- Overrides key: `localStorage` `grove.projects`, a JSON `Record<string, string | null>` keyed by worktree dir. Every read and write wrapped in try/catch.
- Project collapse state lives in the existing `collapsed` list as `project:<name>`.
- Folder name means the last path element of `dir` (as `siblings()` in `removal.ts`), never `w.name` (which carries a repo's `prefix/`).
- A repo's main checkout is its first row in the snapshot. Manual `[[workspace]]` rows (`repoPath === ''`) are never in a project.
- Project header colour `--idle`. The `main` tag: `--muted` text, `1px solid var(--border-strong)` border, 10px, as in the approved mockup.
- British spelling in comments and UI copy; no em-dashes.

## Review Focus

1. **A repo with a `prefix`:** `w.name` is `android/eng-412` but the folder is `eng-412`; grouping must use the folder. Pinned in Task 1.
2. **A worktree being removed:** rows mid-removal stay listed (`rm.withRemovals`); membership must be computed over those listed rows so the group doesn't dissolve under the spinner. Pinned in Task 3 (membership built from `listed`).
3. **Corrupt `grove.projects`:** bad JSON or a non-object loads as `{}` instead of breaking the sidebar. Pinned in Task 1.
4. **A first snapshot that is empty or failed:** must not wipe the saved overrides. Pinned in Task 1 (`prune` with no rows returns the overrides unchanged).
5. **The list cursor after membership changes:** a row moving into or out of a project keeps the cursor on the same dir. Task 3 adds membership to the cursor effect's dependencies.

---

### Task 1: `projects.ts`, membership and overrides

**Files:**
- Create: `frontend/src/projects.ts`
- Test: `frontend/src/projects.test.ts`

**Interfaces:**
- Produces:
  - `type Overrides = Record<string, string | null>`
  - `folder(dir: string): string`, the dir's last element (trailing slashes ignored)
  - `mains(all: main.WorkspaceInfo[]): Set<string>`, dirs of each repo's first row (rows with `repoPath === ''` skipped)
  - `membership(all: main.WorkspaceInfo[], overrides: Overrides): Map<string, string>`, dir → project name
  - `projectNames(member: Map<string, string>): string[]`, distinct names, sorted
  - `rowName(w: main.WorkspaceInfo, project: string): string`, `w.repo`, or `` `${w.repo} · ${folder(w.dir)}` `` when the folder differs from `project`
  - `prune(overrides: Overrides, all: main.WorkspaceInfo[]): Overrides`
  - `loadOverrides(): Overrides`, `saveOverrides(o: Overrides): void`

- [ ] **Step 1: Write the failing tests** in `projects.test.ts`, with a fixture like `repos.test.ts`'s: `ws(repoPath, folder)` giving `dir: `${repoPath}/${folder}``, `repo: repoPath.slice(1)`, `name: folder`. Rows: `/a/main, /a/eng-1, /a/spike, /b/main, /b/eng-1, /c/main, /c/eng-1, /c/solo`.

```ts
test('the same folder in two or more repos is a project; in one repo it is not', () => {
  const m = membership(rows, {})
  assert.deepEqual([...m].sort(), [['/a/eng-1', 'eng-1'], ['/b/eng-1', 'eng-1'], ['/c/eng-1', 'eng-1']])
})
test('main checkouts never group by folder, and mains picks each repo\'s first row', () => {
  assert.deepEqual([...mains(rows)], ['/a/main', '/b/main', '/c/main'])
  assert.equal(membership(rows, {}).has('/a/main'), false)
  assert.deepEqual([...mains([ws('', 'notes'), ...rows.slice(0, 1)])], ['/a/main'], 'manual rows have no main')
})
test('the folder, not the prefixed name, decides', () => {
  const prefixed = [ws('/a', 'main'), { ...ws('/a', 'x'), name: 'android/x' }, ws('/b', 'main'), { ...ws('/b', 'x'), name: 'ios/x' }]
  assert.equal(membership(prefixed, {}).get('/a/x'), 'x')
})
test('an override adds a worktree with another folder name, and counts towards its project', () => {
  assert.equal(membership(rows, { '/c/solo': 'eng-1' }).get('/c/solo'), 'eng-1')
  const two = [ws('/a', 'main'), ws('/a', 'eng-1'), ws('/b', 'main'), ws('/b', 'other')]
  assert.equal(membership(two, { '/b/other': 'eng-1' }).get('/a/eng-1'), 'eng-1', 'the override member makes two repos')
})
test('a null override removes a derived member; a project left in one repo dissolves', () => {
  const two = rows.filter((w) => w.repoPath !== '/c')
  const m = membership(two, { '/b/eng-1': null })
  assert.equal(m.size, 0)
})
test('a project made by override may have one member', () => {
  assert.deepEqual([...membership(rows, { '/a/spike': 'research' })].filter(([, p]) => p === 'research'), [['/a/spike', 'research']])
})
test('manual rows are never members, even with an override', () => {
  assert.equal(membership([...rows, ws('', 'eng-1')], { '/manual/eng-1': 'eng-1' }).has('/manual/eng-1'), false)
})
test('rowName shows the repo, and the folder when it differs from the project', () => {
  assert.equal(rowName(ws('/a', 'eng-1'), 'eng-1'), 'a')
  assert.equal(rowName(ws('/a', 'spike'), 'eng-1'), 'a · spike')
})
test('prune drops missing dirs, keeps the rest, and leaves everything when there are no rows', () => {
  const o = { '/a/spike': 'x', '/gone': null }
  assert.deepEqual(prune(o, rows), { '/a/spike': 'x' })
  assert.deepEqual(prune(o, []), o)
})
test('corrupt saved overrides load as empty', () => {
  globalThis.localStorage = { getItem: () => '{nope', setItem() {} } as unknown as Storage
  assert.deepEqual(loadOverrides(), {})
  globalThis.localStorage = { getItem: () => '[1]', setItem() {} } as unknown as Storage
  assert.deepEqual(loadOverrides(), {})
})
```

(The manual fixture row uses `dir: '/manual/eng-1'`, as `repos.test.ts` does for `repoPath ''`.)

- [ ] **Step 2: Run** `cd frontend && npm test`. Expected: FAIL, `projects.ts` not found.

- [ ] **Step 3: Implement `projects.ts`.** `membership`:

```
skip rows with repoPath '' throughout
overridden = rows whose dir is a key of overrides
for each overridden row with a string value: out[dir] = value
candidates = rows not overridden and not in mains(all)
for each folder f among candidates:
  repos = repoPaths of candidates in f ∪ repoPaths of override members whose value === f
  if repos.size >= 2: out[dir] = f for every candidate in f
```

`loadOverrides` returns `{}` unless the parsed value is a non-null, non-array object.

- [ ] **Step 4: Run** `npm test`. Expected: PASS.

- [ ] **Step 5: Commit** `git add src/projects.ts src/projects.test.ts && git commit -m "feat: work out sidebar projects from folder names and overrides"`

### Task 2: `repos.ts`, project groups first

**Files:**
- Modify: `frontend/src/repos.ts`
- Test: `frontend/src/repos.test.ts`

**Interfaces:**
- Consumes: `membership`'s `Map<string, string>` (Task 1).
- Produces:
  - `type Group = { repo: main.RepoInfo | null; project?: string; rows: main.WorkspaceInfo[] }`
  - `groups(rows, repos, collapsed: string[], q: string, member: Map<string, string> = new Map()): Group[]`
  - `matches(w, q, project = ''): boolean`, also true when `project` contains `q`
  - `numbered(rows, repos, member): main.WorkspaceInfo[]`, rows in display order with nothing collapsed and no filter
  - `projectKey(name: string): string`, returns `` `project:${name}` ``

- [ ] **Step 1: Write the failing tests**, reusing `repos.test.ts`'s fixtures. `const member = new Map([['/a/fix', 'p'], ['/b/main', 'p']])`; a project group has `repo: null` and `project`, so extend `shape` to `g.project ?? g.repo?.path ?? null`.

```ts
test('project groups come first, sorted, and their rows leave the repo groups', () => {
  const m = new Map([['/a/fix', 'zed'], ['/b/main', 'alpha']])
  assert.deepEqual(shape(groups(rows, [a, b], [], '', m)), [
    ['alpha', ['/b/main']], ['zed', ['/a/fix']], ['/a', ['/a/main']], ['/b', []], [null, ['/manual/notes']],
  ])
})
test('filtering by project name keeps the whole group', () => {
  assert.deepEqual(shape(groups(rows, [a, b], [], 'p', member)).slice(0, 1), [['p', ['/a/fix', '/b/main']]])
})
test('a collapsed project lists no rows', () => {
  assert.deepEqual(shape(groups(rows, [a, b], [projectKey('p')], '', member))[0], ['p', []])
})
test('numbered follows display order whatever is collapsed', () => {
  assert.deepEqual(numbered(rows, [a, b], member).map((w) => w.dir), ['/a/fix', '/b/main', '/a/main', '/manual/notes'])
})
```

Existing tests must still pass unchanged (the default `member` keeps old behaviour).

- [ ] **Step 2: Run** `npm test`. Expected: FAIL on the new tests.

- [ ] **Step 3: Implement.** Project groups are built from the rows in `member`, ordered by `projectNames`, with the same collapse and filter rules as repo groups (collapse key `projectKey(name)`). A project group is dropped when filtering leaves it empty, and also when it has no rows at all. Keep the doc comment above `groups` accurate.

- [ ] **Step 4: Run** `npm test`. Expected: PASS.

- [ ] **Step 5: Commit** `git commit -am "feat: put project groups first in the sidebar list"`

### Task 3: Show projects and the main tag

**Files:**
- Modify: `frontend/src/App.tsx` (state near line 71; `groups` call at 83; ⌘1–9 at 455-461; cursor effect at 560-566; `<Sidebar>` props near 634)
- Modify: `frontend/src/Sidebar.tsx`
- Modify: `frontend/src/style.css` (after line 56)

**Interfaces:**
- Consumes: `loadOverrides`, `saveOverrides`, `prune`, `membership`, `mains`, `rowName` (Task 1); `groups`, `numbered`, `projectKey` (Task 2).
- Produces for Task 4: `overrides` / `setOverrides` state in `App`; new `Sidebar` props `member: Map<string, string>`, `mains: Set<string>`, `numbered: main.WorkspaceInfo[]`.

- [ ] **Step 1: `App.tsx`.**
  - `const [overrides, setOverrides] = useState(loadOverrides)`. An effect on `overrides` calls `saveOverrides`.
  - An effect on `snap` sets `prune(overrides, snap.workspaces ?? [])` when it differs (compare key counts).
  - `const member = useMemo(() => membership(listed, overrides), ...)`. Build it from `listed`, not `all` (Review Focus 2). Pass it to `groups` and to `numbered(listed, repos, member)`.
  - ⌘1–9: `const w = order[n]`, where `order` is the `numbered` result. The hidden message names `member.get(w.dir) ?? w.repo` and the collapse check uses `projectKey(project)` for members.
  - Add `[...member].join('\n')` to the cursor effect's dependency list (Review Focus 5).
  - `onToggle` already takes a string, so project headers pass `projectKey(name)`.
- [ ] **Step 2: `Sidebar.tsx`.**
  - Groups with `project` render a header `<li className="group project">` with the name, the "N hidden" tag (count of `member` entries with that name when collapsed) and a ⋯ menu with only Collapse / Expand. The React key is `projectKey(name)`.
  - Rows in a project group show `rowName(w, g.project)` as the name.
  - Rows whose dir is in `mains` get `<span className="tag-main">main</span>` after the name.
  - Numbers come from `props.numbered.findIndex((x) => x.dir === w.dir)`.
- [ ] **Step 3: `style.css`.** `.list li.group.project` takes `color: var(--idle)`. `.tag-main` is `font-size: 10px; color: var(--muted); border: 1px solid var(--border-strong); border-radius: 4px; padding: 0 4px; margin-left: 6px`.
- [ ] **Step 4: Verify** `npm test && npm run build`. Expected: tests PASS and `tsc` reports no errors. Then `make dev` with two repos sharing a worktree folder name. Expected:
  - the project group is on top, its rows are named by repo, and both main checkouts show `main`
  - ⌘1 opens the first project row
  - collapsing the project hides its rows and shows "N hidden"
- [ ] **Step 5: Commit** `git commit -am "feat: show cross-repo projects and main checkouts in the sidebar"`

### Task 4: Add to and remove from projects

**Files:**
- Modify: `frontend/src/Sidebar.tsx` (`rowMenu`, about line 56)
- Modify: `frontend/src/App.tsx` (`Modal` union at line 18-27; modal rendering near 738; a new `ProjectForm` next to `WorktreeForm`)

**Interfaces:**
- Consumes: `overrides` / `setOverrides`, `member` (Task 3); `projectNames` (Task 1).
- Produces: `Sidebar` props `onProject: (dir: string, name: string | null) => void` and `onNewProject: (dir: string) => void`; modal `{ kind: 'project'; dir: string }`.

- [ ] **Step 1: Row menu.** For rows with a `repoPath`, append to `rowMenu`'s items:
  - `'divider'`
  - one `{ label: `Add to ${name}` }` per `projectNames(member)` except the row's own project
  - `New project…`
  - `Remove from project`, only when `member.has(w.dir)`
  
  Add calls `onProject(dir, name)`, Remove calls `onProject(dir, null)`. In `App`, `onProject` is `setOverrides((o) => ({ ...o, [dir]: name }))`.
- [ ] **Step 2: `ProjectForm`.** Copy `WorktreeForm`'s shell: `overlay`, `dialog form`, `keepFocus`, Esc cancels. It has the heading "New project" and one autofocused "Name" input. **Create** is disabled while the trimmed name is empty or already in `projectNames(member)`. Submit calls `onProject(dir, name.trim())` and closes.
- [ ] **Step 3: Verify** `npm test && npm run build`, then in the running app:
  - "New project…" on a lone worktree creates a one-row project
  - "Add to <project>" moves a second worktree in
  - "Remove from project" on a derived member of a two-repo project dissolves it, and both rows go back to their repos
  - after relaunching, the overrides are still there
- [ ] **Step 4: Commit** `git commit -am "feat: add worktrees to and remove them from sidebar projects"`
