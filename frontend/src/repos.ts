import type { main } from '../wailsjs/go/models'
import { projectNames } from './projects.ts'

// Group is one header's rows in the sidebar: a project's (project set, repo
// null), a repo's, or, with neither, the manual [[workspace]] entries.
export type Group = { repo: main.RepoInfo | null; project?: string; rows: main.WorkspaceInfo[] }

// projectKey is a project's entry in the collapsed list, beside repo paths.
export const projectKey = (name: string) => `project:${name}`

// matches is the worktree filter: q (trimmed, lower case) in the name, branch or
// project name.
export const matches = (w: main.WorkspaceInfo, q: string, project = '') =>
  !q || w.name.toLowerCase().includes(q) || w.branch.toLowerCase().includes(q) || project.toLowerCase().includes(q)

// groups sorts rows into one group per project (member: dir to project), by
// name, then one per repo in the window's order without the project rows, then
// the manual entries. A collapsed group lists no rows, except while filtering,
// when every group lists just its matches and one without any goes. Rows of a
// repo no longer in the window go too.
export function groups(rows: main.WorkspaceInfo[], repos: main.RepoInfo[], collapsed: string[], q: string, member = new Map<string, string>()): Group[] {
  const inWindow = new Set(repos.map((r) => r.path))
  const projects: Group[] = projectNames(member).map((project) => ({ repo: null, project, rows: [] }))
  const out: Group[] = [...repos.map((repo) => ({ repo, rows: [] })), { repo: null, rows: [] }]
  for (const w of rows) {
    const project = member.get(w.dir)
    const g = project !== undefined && inWindow.has(w.repoPath) ? projects.find((g) => g.project === project) : out.find((g) => !g.project && (g.repo?.path ?? '') === w.repoPath)
    const key = project !== undefined ? projectKey(project) : w.repoPath
    if (g && matches(w, q, project) && (q || !collapsed.includes(key))) g.rows.push(w)
  }
  // A project with no rows left in the window has nothing to show.
  const live = new Set(rows.filter((w) => member.has(w.dir) && inWindow.has(w.repoPath)).map((w) => member.get(w.dir)))
  return [...projects.filter((g) => live.has(g.project) && (g.rows.length > 0 || !q)), ...out.filter((g) => g.rows.length > 0 || (g.repo && !q))]
}

// numbered is the order ⌘1–9 count in: the rows as listed with nothing collapsed
// and no filter.
export const numbered = (rows: main.WorkspaceInfo[], repos: main.RepoInfo[], member: Map<string, string>) =>
  groups(rows, repos, [], '', member).flatMap((g) => g.rows)

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

// removeText is the inline confirm for taking a repo out of the window: what
// stays on disk, then what closes, from its worktrees' open tabs (left out when
// nothing is open). Everything from the repo's config goes with it (its other
// [[repo]]s and [[workspace]]s), so all of that counts.
export function removeText(repo: main.RepoInfo, all: main.WorkspaceInfo[]) {
  const worktrees = all.filter((w) => w.config === repo.config)
  const tabs = worktrees.flatMap((w) => w.panes ?? [])
  const claude = tabs.filter((t) => t.kind === 'claude').length
  const closing = [
    claude > 0 && count(claude, 'open Claude tab', 'open Claude tabs'),
    tabs.length > claude && count(tabs.length - claude, 'shell', 'shells'),
  ].filter(Boolean)
  const n = worktrees.length
  const kept = `Its ${count(n, 'worktree and branch', 'worktrees and branches')} stay on disk.`
  return {
    title: `Remove ${repo.name} from this window?`,
    detail: closing.length ? `${kept} ${closing.join(' and ')} will close.` : kept,
  }
}
