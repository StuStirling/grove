// The tab layout of one worktree: one pane of tabs, or two side by side. Pure
// functions that return new layouts, so the rules live in one place and are
// unit-tested (layout.test.ts).

// Tab is what the layout needs of a backend pane.
export type Tab = { id: string; name: string }
export type PaneState = { tabs: string[]; active: string | null } // tabs are backend pane ids
export type Layout = {
  panes: PaneState[] // 1 or 2: left, then right
  focus: number // index of the focused pane
  ratio: number // the left pane's share of the width while split, 0..1
  zoom: boolean // the focused pane fills the worktree (only while split)
  labels: Record<string, string> // tab id -> label, e.g. "zsh 2"
}

// norm keeps a layout valid: a pane with no tabs goes (so a split undoes
// itself), every pane's active tab is one of its tabs, focus points at a pane,
// zoom needs a split, and labels cover only the tabs still there. No tabs at all
// is one empty pane.
function norm(l: Layout): Layout {
  let panes: PaneState[] = l.panes
    .filter((p) => p.tabs.length > 0)
    .map((p) => ({ tabs: p.tabs, active: p.active && p.tabs.includes(p.active) ? p.active : p.tabs[0] }))
  if (panes.length === 0) panes = [{ tabs: [], active: null }]
  const labels: Record<string, string> = {}
  for (const id of panes.flatMap((p) => p.tabs)) labels[id] = l.labels[id]
  return { ...l, panes, focus: Math.min(l.focus, panes.length - 1), zoom: l.zoom && panes.length > 1, labels }
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
// gone close (as closeTab), new ones join the focused pane. The first time (the
// worktree's configured panes), the first tab goes in the left pane and the rest
// in a right one: claude | zsh.
export function sync(prev: Layout | undefined, live: Tab[], ratio: number): Layout {
  if (!prev) {
    const ids = live.map((t) => t.id)
    let l: Layout = { panes: [{ tabs: ids.slice(0, 1), active: null }, { tabs: ids.slice(1), active: null }], focus: 0, ratio, zoom: false, labels: {} }
    for (const t of live) l = label(l, t)
    return norm(l)
  }
  let l = prev
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
// end), making the right pane if i is past the last, and makes it the focused
// tab. Placing twice changes nothing, so it doesn't matter whether sync or the
// caller sees a new tab first.
export function place(l: Layout, t: Tab, i: number, index?: number): Layout {
  return put(label(l, t), t.id, i, index)
}

function put(l: Layout, id: string, i: number, index?: number): Layout {
  const was = l.panes[i]?.tabs.indexOf(id) ?? -1
  const panes = l.panes.map((p) => without(p, id))
  if (i >= panes.length) {
    i = panes.length
    panes.push({ tabs: [], active: null })
  }
  const tabs = [...panes[i].tabs]
  tabs.splice(index ?? (was < 0 ? tabs.length : was), 0, id)
  panes[i] = { tabs, active: id }
  return norm({ ...l, panes, focus: i })
}

// without takes a tab out of a pane; if it was in front, the tab to its right
// (else its left) takes its place.
function without(p: PaneState, id: string): PaneState {
  const k = p.tabs.indexOf(id)
  if (k < 0) return p
  const tabs = p.tabs.filter((t) => t !== id)
  return { tabs, active: p.active === id ? (tabs[k] ?? tabs[k - 1] ?? null) : p.active }
}

// activate shows a tab in its pane and focuses that pane.
export function activate(l: Layout, id: string): Layout {
  const i = l.panes.findIndex((p) => p.tabs.includes(id))
  return i < 0 ? l : norm({ ...l, focus: i, panes: l.panes.map((p, j) => (j === i ? { ...p, active: id } : p)) })
}

// closeTab removes a tab; if it was in front, the tab to its right (else its
// left) takes its place.
export const closeTab = (l: Layout, id: string) => norm({ ...l, panes: l.panes.map((p) => without(p, id)) })

// cycle moves through the focused pane's tabs, wrapping around.
export function cycle(l: Layout, d: number): Layout {
  const { tabs, active } = l.panes[l.focus]
  if (tabs.length === 0) return l
  return activate(l, tabs[(tabs.indexOf(active!) + d + tabs.length) % tabs.length])
}

// focusPane focuses pane i (the only one when not split).
export const focusPane = (l: Layout, i: number) => norm({ ...l, focus: i })

// split moves the focused pane's tab into a new right pane and focuses it. With
// a single tab there is nothing to move, so it returns null: the caller opens a
// shell and places it in pane 1. Already split: no change.
export function split(l: Layout): Layout | null {
  if (l.panes.length > 1) return l
  const { tabs, active } = l.panes[0]
  return tabs.length < 2 ? null : put(l, active!, 1)
}

// moveToOther moves pane i's tab to the other pane; a pane left empty goes.
export function moveToOther(l: Layout, i: number): Layout {
  const id = l.panes[i]?.active
  return l.panes.length < 2 || !id ? l : put(l, id, 1 - i)
}

// closePane unsplits: pane i's tabs join the end of the other pane, which keeps
// its tab in front and takes focus.
export function closePane(l: Layout, i: number): Layout {
  if (l.panes.length < 2) return l
  const keep = l.panes[1 - i]
  return norm({ ...l, panes: [{ tabs: [...keep.tabs, ...l.panes[i].tabs], active: keep.active }], focus: 0 })
}

export const toggleZoom = (l: Layout) => norm({ ...l, zoom: !l.zoom })

// focusedTab is the tab keyboard input goes to.
export const focusedTab = (l: Layout | undefined) => l?.panes[l.focus].active ?? null
