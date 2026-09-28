import type { main } from '../wailsjs/go/models'

// Group is one repo's rows in the sidebar, under its header. repo null holds the
// manual [[workspace]] entries, last, under a plain heading.
export type Group = { repo: main.RepoInfo | null; rows: main.WorkspaceInfo[] }

// matches is the worktree filter: q (trimmed, lower case) in the name or branch.
export const matches = (w: main.WorkspaceInfo, q: string) => !q || w.name.toLowerCase().includes(q) || w.branch.toLowerCase().includes(q)

// groups sorts rows into one group per repo, in the window's order, then the
// manual entries. A collapsed group lists no rows, except while filtering, when
// every group lists just its matches and one without any goes. Rows of a repo no
// longer in the window go too.
export function groups(rows: main.WorkspaceInfo[], repos: main.RepoInfo[], collapsed: string[], q: string): Group[] {
  const out: Group[] = [...repos.map((repo) => ({ repo, rows: [] })), { repo: null, rows: [] }]
  for (const w of rows) {
    const g = out.find((g) => (g.repo?.path ?? '') === w.repoPath)
    if (g && matches(w, q) && (q || !collapsed.includes(w.repoPath))) g.rows.push(w)
  }
  return out.filter((g) => g.rows.length > 0 || (g.repo && !q))
}

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
