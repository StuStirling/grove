import type { main } from '../wailsjs/go/models'

// Overrides are the hand-made exceptions to grouping by folder name, by
// worktree dir: a project name puts the worktree in that project, null keeps it
// out of every project.
export type Overrides = Record<string, string | null>

const KEY = 'grove.projects'

// folder is a worktree's folder name: work started across repos shares it.
export const folder = (dir: string) => dir.replace(/\/+$/, '').split('/').pop() ?? ''

// mains are each repo's main checkout: git lists it first, so it is the repo's
// first row in the snapshot.
export function mains(all: main.WorkspaceInfo[]): Set<string> {
  const seen = new Set<string>()
  const out = new Set<string>()
  for (const w of all) {
    if (!w.repoPath || seen.has(w.repoPath)) continue
    seen.add(w.repoPath)
    out.add(w.dir)
  }
  return out
}

// membership maps a worktree's dir to its project. The same folder in two or
// more repos is a project, main checkouts aside; overrides beat the folder, and
// a worktree added by override counts towards its project's repos.
export function membership(all: main.WorkspaceInfo[], overrides: Overrides): Map<string, string> {
  const out = new Map<string, string>()
  const repos = new Map<string, Set<string>>() // project name -> repo paths
  const add = (name: string, repoPath: string) => repos.set(name, (repos.get(name) ?? new Set()).add(repoPath))
  const mainDirs = mains(all)
  const candidates: main.WorkspaceInfo[] = []
  for (const w of all) {
    if (!w.repoPath) continue
    if (w.dir in overrides) {
      const name = overrides[w.dir]
      if (name) out.set(w.dir, name), add(name, w.repoPath)
    } else if (!mainDirs.has(w.dir)) candidates.push(w)
  }
  for (const w of candidates) add(folder(w.dir), w.repoPath)
  for (const w of candidates) if (repos.get(folder(w.dir))!.size >= 2) out.set(w.dir, folder(w.dir))
  return out
}

export const projectNames = (member: Map<string, string>) => [...new Set(member.values())].sort()

// rowName is a project row's name: its repo, and its folder when that isn't the
// project's name.
export const rowName = (w: main.WorkspaceInfo, project: string) => (folder(w.dir) === project ? w.repo : `${w.repo} · ${folder(w.dir)}`)

// prune drops overrides for worktrees that are gone. No rows means nothing has
// loaded, not that every worktree went, so it keeps them all.
export function prune(overrides: Overrides, all: main.WorkspaceInfo[]): Overrides {
  if (!all.length) return overrides
  const dirs = new Set(all.map((w) => w.dir))
  return Object.fromEntries(Object.entries(overrides).filter(([dir]) => dirs.has(dir)))
}

export function loadOverrides(): Overrides {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '{}')
    return v && typeof v === 'object' && !Array.isArray(v) ? v : {}
  } catch {
    return {}
  }
}

export function saveOverrides(o: Overrides) {
  try {
    localStorage.setItem(KEY, JSON.stringify(o))
  } catch {
    // ponytail: not saved this time (private mode, quota); the next change tries again
  }
}
