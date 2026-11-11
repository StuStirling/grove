import type { Layout } from './layout'

// Each open worktree's layout, kept so a worktree restored at launch (its tabs
// back under the same ids) reopens split and ordered as it was.
// ponytail: keys are never pruned; a few hundred bytes per worktree.
const KEY = 'grove.layout.' // + worktree dir

export function savedLayout(dir: string): Layout | undefined {
  try {
    const v = JSON.parse(localStorage.getItem(KEY + dir) ?? 'null')
    return v && Array.isArray(v.panes) ? v : undefined
  } catch {
    return undefined
  }
}

export function saveLayouts(ls: Record<string, Layout>) {
  try {
    for (const [dir, l] of Object.entries(ls)) localStorage.setItem(KEY + dir, JSON.stringify(l))
  } catch {
    // not saved (private mode, quota): only a restored layout is lost
  }
}

// startLayout is what a worktree's layout syncs from: the one in memory, else,
// for a worktree restored at launch, the saved one. The restored gate keeps an
// older run's layout off unrelated tabs that reuse its ids.
export const startLayout = (prev: Layout | undefined, restored: boolean, dir: string) =>
  prev ?? (restored ? savedLayout(dir) : undefined)
