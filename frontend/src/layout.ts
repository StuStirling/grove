// The tab layout of one worktree. Pure functions that return new layouts, so
// the rules live in one place and are unit-tested (layout.test.ts).

// Tab is what the layout needs of a backend pane.
export type Tab = { id: string; name: string }
export type PaneState = { tabs: string[]; active: string | null } // tabs are backend pane ids
export type Layout = {
  panes: PaneState[]
  focus: number // index of the focused pane
  labels: Record<string, string> // tab id -> label, e.g. "zsh 2"
}

// norm keeps a layout valid: every pane's active tab is one of its tabs and
// focus points at a pane; labels cover only the tabs still there.
function norm(l: Layout): Layout {
  const panes = l.panes.map((p) => ({ tabs: p.tabs, active: p.active && p.tabs.includes(p.active) ? p.active : (p.tabs[0] ?? null) }))
  const labels: Record<string, string> = {}
  for (const id of panes.flatMap((p) => p.tabs)) labels[id] = l.labels[id]
  return { ...l, panes, focus: Math.min(l.focus, panes.length - 1), labels }
}

// label names a tab the first time it's seen: its backend name, numbered from 2
// while another of the worktree's tabs has that label ("zsh", "zsh 2", ...).
// Labels are never recomputed, so "zsh 2" stays "zsh 2" when "zsh" closes.
function label(l: Layout, t: Tab): Layout {
  if (l.labels[t.id]) return l
  const taken = new Set(Object.values(l.labels))
  let name = t.name
  for (let n = 2; taken.has(name); n++) name = `${t.name} ${n}`
  return { ...l, labels: { ...l.labels, [t.id]: name } }
}

const tabsOf = (l: Layout) => l.panes.flatMap((p) => p.tabs)

// sync brings a layout in line with the worktree's live tabs: tabs that are
// gone close (as closeTab), new ones join the focused pane. The first time,
// every tab goes in one pane.
export function sync(prev: Layout | undefined, live: Tab[]): Layout {
  let l = prev ?? { panes: [{ tabs: [], active: null }], focus: 0, labels: {} }
  const ids = new Set(live.map((t) => t.id))
  for (const id of tabsOf(l)) if (!ids.has(id)) l = closeTab(l, id)
  const known = new Set(tabsOf(l))
  for (const t of live) {
    if (known.has(t.id)) continue
    l = label(l, t)
    l = { ...l, panes: l.panes.map((p, i) => (i === l.focus ? { ...p, tabs: [...p.tabs, t.id] } : p)) }
  }
  return norm(l)
}

// place puts a tab in pane i (at index, else where it already is, else at the
// end) and makes it the focused tab. Placing twice changes nothing, so it
// doesn't matter whether sync or the caller sees a new tab first.
export function place(l: Layout, t: Tab, i: number, index?: number): Layout {
  return put(label(l, t), t.id, i, index)
}

function put(l: Layout, id: string, i: number, index?: number): Layout {
  const panes = l.panes.map((p) => ({ ...p, tabs: [...p.tabs] }))
  const dest = panes[i]
  const was = dest.tabs.indexOf(id)
  for (const p of panes) p.tabs = p.tabs.filter((t) => t !== id)
  dest.tabs.splice(index ?? (was < 0 ? dest.tabs.length : was), 0, id)
  dest.active = id
  return norm({ ...l, panes, focus: i })
}

// activate shows a tab in its pane and focuses that pane.
export function activate(l: Layout, id: string): Layout {
  const i = l.panes.findIndex((p) => p.tabs.includes(id))
  return i < 0 ? l : norm({ ...l, focus: i, panes: l.panes.map((p, j) => (j === i ? { ...p, active: id } : p)) })
}

// closeTab removes a tab; if it was showing, its pane shows the tab to its
// right, else the one to its left.
export function closeTab(l: Layout, id: string): Layout {
  const panes = l.panes.map((p) => {
    const k = p.tabs.indexOf(id)
    if (k < 0) return p
    const tabs = p.tabs.filter((t) => t !== id)
    return { tabs, active: p.active === id ? (tabs[k] ?? tabs[k - 1] ?? null) : p.active }
  })
  return norm({ ...l, panes })
}

// cycle moves through the focused pane's tabs, wrapping around.
export function cycle(l: Layout, d: number): Layout {
  const { tabs, active } = l.panes[l.focus]
  if (tabs.length === 0) return l
  return activate(l, tabs[(tabs.indexOf(active!) + d + tabs.length) % tabs.length])
}

// focusedTab is the tab keyboard input goes to.
export const focusedTab = (l: Layout | undefined) => l?.panes[l.focus].active ?? null
