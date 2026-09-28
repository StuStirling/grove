import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime'
import type { main } from '../wailsjs/go/models'
import { WorkspaceHeader, WorkspaceView, terms } from './Panes'
import { Sidebar, sidebarWidth, type RemovalAction } from './Sidebar'
import * as rm from './removal'
import { groups } from './repos'
import { cycle, focusPane, focusedTab, place, split, sync, toggleZoom, type Layout } from './layout'
import { errText, key } from './util'

// Worktrees are keyed by dir everywhere: two repos in one window can share
// worktree names.
type Confirm = { kind: 'confirm'; title: string; detail?: string; danger?: boolean; resolve: (yes: boolean) => void }
type Modal = { kind: 'new' | 'checkout'; repo: string } | { kind: 'help' } | Confirm

const FONT_KEY = 'grove.fontDelta'
const SIDEBAR_KEY = 'grove.sidebarWidth'
const SPLIT_KEY = 'grove.split:' // + worktree dir: its split ratio
const COLLAPSED_KEY = 'grove.collapsed' // repo paths whose sidebar groups are collapsed
const MONO = 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Monaco, monospace'

const savedRatio = (dir: string) => {
  const r = Number(localStorage.getItem(SPLIT_KEY + dir))
  return r > 0 && r < 1 ? r : 0.5
}

const savedCollapsed = (): string[] => {
  try {
    const v = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

// Focus a pane's terminal, waiting a few frames for it to mount and for a
// closing dialog to unmount. A dialog that stays open keeps focus.
function focusTerm(id: string, tries = 30) {
  const t = terms.get(id)
  if (t && !document.querySelector('[role=dialog]')) t.focus()
  else if (tries > 0) requestAnimationFrame(() => focusTerm(id, tries - 1))
}

export default function App() {
  const [snap, setSnap] = useState<main.Snapshot | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const [layouts, setLayouts] = useState<Record<string, Layout>>({}) // open worktree -> its tabs
  const [focusReq, setFocusReq] = useState(0)
  const [filter, setFilter] = useState('')
  const [cursor, setCursor] = useState(0)
  const [listFocused, setListFocused] = useState(false)
  const [modal, setModal] = useState<Modal | null>(null)
  const [busy, setBusy] = useState('')
  const [msg, setMsg] = useState<{ text: string; err?: boolean }>({ text: '' })
  const [fontDelta, setFontDelta] = useState(() => Number(localStorage.getItem(FONT_KEY)) || 0)
  const [removals, setRemovals] = useState<rm.Removals>({})
  const [sidebarW, setSidebarW] = useState(() => sidebarWidth(Number(localStorage.getItem(SIDEBAR_KEY)) || 250))
  const [collapsed, setCollapsed] = useState(savedCollapsed)
  // The repo whose Remove from window is being confirmed in the sidebar, and
  // where focus was when it was asked.
  const [repoConfirm, setRepoConfirm] = useState<{ path: string; back: Element | null } | null>(null)
  const filterRef = useRef<HTMLInputElement>(null)
  const snapSeq = useRef({ asked: 0, shown: 0 })

  const all = snap?.workspaces ?? []
  const repos = snap?.repos ?? []
  const q = filter.trim().toLowerCase()
  // Rows being removed stay in the list, where they were, until they're done.
  const listed = rm.withRemovals(all, removals)
  const grouped = groups(listed, repos, collapsed, q)
  // The rows shown: the list cursor never lands on a hidden one, nor on one
  // whose group the remove confirm stands in for.
  const filtered = grouped.flatMap((g) => (g.repo && g.repo.path === repoConfirm?.path ? [] : g.rows))
  const sel = all.find((w) => w.dir === selected)
  const selTab = focusedTab(sel?.open ? layouts[sel.dir] : undefined)
  const font = {
    family: snap?.fontFamily || MONO,
    size: Math.min(32, Math.max(8, (snap?.fontSize || 13) + fontDelta)),
  }

  const say = (text: string, err = false) => setMsg({ text, err })

  // fetchSnap shows a snapshot and brings each open worktree's layout in line
  // with its tabs, and the removal rows with its worktrees; a closed worktree's
  // layout goes, so it reopens fresh. A reply older than one already shown is
  // dropped: it would undo a newer tab change.
  async function fetchSnap(get: () => Promise<main.Snapshot>) {
    const n = ++snapSeq.current.asked
    const s = await get()
    if (n < snapSeq.current.shown) return
    snapSeq.current.shown = n
    setSnap(s)
    setLayouts((ls) => {
      const next: Record<string, Layout> = {}
      for (const w of s.workspaces ?? []) if (w.open) next[w.dir] = sync(ls[w.dir], w.panes ?? [], savedRatio(w.dir))
      return next
    })
    setRemovals((rs) => rm.sync(rs, s.workspaces ?? []))
  }
  const refresh = () => fetchSnap(api.State)
  const reload = async (manual: boolean) => {
    await fetchSnap(api.Reload)
    if (manual) say('refreshed')
  }
  const setLayout = (dir: string, f: (l: Layout) => Layout) => setLayouts((ls) => (ls[dir] ? { ...ls, [dir]: f(ls[dir]) } : ls))
  // focusNow puts keyboard focus on the selected worktree's focused tab once
  // the next render is on screen.
  const focusNow = () => setFocusReq((n) => n + 1)

  // run shows the status-bar spinner while a blocking git operation is in flight.
  // grove's own actions are ignored meanwhile, so it can't be re-triggered; the
  // panes stay usable. Errors go to onErr (a form) or the status bar.
  async function run<T>(label: string, fn: () => Promise<T>, onErr?: (e: string) => void): Promise<T | undefined> {
    setBusy(label)
    say('')
    try {
      return await fn()
    } catch (e) {
      onErr ? onErr(errText(e)) : say(errText(e), true)
      return undefined
    } finally {
      setBusy('')
    }
  }

  const confirm = (title: string, detail?: string, danger?: boolean) =>
    new Promise<boolean>((resolve) => setModal({ kind: 'confirm', title, detail, danger, resolve }))

  // openWs starts the panes of the workspace at dir if needed and switches to it.
  async function openWs(dir: string) {
    // Not a worktree being removed. A gone row's dir is a new worktree's, which
    // may open (just made) before its snapshot drops the row.
    if (!rm.usable(removals[dir]) && !removals[dir].gone) return
    try {
      await api.Open(dir)
      setSelected(dir)
      setFilter('')
      setListFocused(false)
      filterRef.current?.blur()
      await refresh()
      focusNow()
    } catch (e) {
      say(errText(e), true)
    }
  }

  function focusList() {
    const i = filtered.findIndex((w) => w.dir === selected)
    setCursor(Math.max(0, i))
    filterRef.current?.focus()
    filterRef.current?.select()
  }

  function leaveList() {
    setFilter('')
    filterRef.current?.blur()
    focusNow()
  }

  // The action target: the list cursor while the sidebar has focus (as the TUI
  // acted on its cursor), else the selected workspace.
  const target = (): main.WorkspaceInfo | undefined => (listFocused ? filtered[cursor] : sel)

  async function closeWs(ws: main.WorkspaceInfo) {
    if (!ws.open) return say(`${ws.name} is not open`)
    if (!(await confirm(`Close ${ws.name}?`, 'Stops its panes. The worktree stays on disk.'))) return say('cancelled')
    try {
      await api.Close(ws.dir)
      say(`closed ${ws.name}`)
      focusList() // ↩ reopens it
    } catch (e) {
      say(errText(e), true)
    }
  }

  // Removal reports in the worktree's own row, not the status bar, and doesn't
  // block other actions: several can run at once.
  async function deleteWs(ws: main.WorkspaceInfo) {
    // A manual [[workspace]] isn't a worktree: Remove refuses it, in its row.
    if (ws.repoPath && !(await confirm(`Delete worktree ${ws.name}?`, `Removes ${ws.dir} and stops its panes.`, true))) return
    setRemovals((rs) => rm.start(rs, ws, listed))
    await removeRow(ws.dir, false)
  }

  async function removeRow(dir: string, force: boolean) {
    let res: main.RemoveResult
    try {
      res = await api.Remove(dir, force)
    } catch (e) {
      res = { status: 'failed', reason: errText(e), detail: errText(e), branchKept: '', branchDetail: '' }
    }
    setRemovals((rs) => rm.result(rs, dir, res, Date.now()))
    if (res.status === 'removed') await reload(false) // drop it from menus; its row stays until done
  }

  async function onRemoval(r: rm.Removal, action: RemovalAction) {
    const dir = r.ws.dir
    switch (action) {
      case 'keep':
        return setRemovals((rs) => rm.keep(rs, dir))
      case 'dismiss':
        return setRemovals((rs) => rm.dismiss(rs, dir))
      case 'force':
        if (!(await confirm(`Force remove ${r.ws.name}?`, 'Its uncommitted changes will be lost.', true))) return
        setRemovals((rs) => rm.force(rs, dir))
        return removeRow(dir, true)
      case 'delete-branch': {
        const unmerged = r.kind === 'kept' && r.reason === 'unmerged'
        const detail = unmerged ? "It isn't merged, so its commits will be lost." : 'Commits only on this branch will be lost.'
        if (!(await confirm(`Delete branch ${r.ws.branch}?`, detail, true))) return
        setRemovals((rs) => rm.deleting(rs, r))
        let res: main.BranchResult
        try {
          res = await api.DeleteBranch(r.ws.repoPath, r.ws.branch, true)
        } catch (e) {
          res = { kept: errText(e), detail: errText(e) }
        }
        setRemovals((rs) => rm.branchResult(rs, dir, res, Date.now()))
      }
    }
  }

  // newTab opens a Claude or shell tab in the worktree at dir (opening the
  // worktree with just that tab if it isn't open) and shows it in pane i, else
  // the focused one.
  async function newTab(dir: string | undefined, kind: 'claude' | 'shell', i?: number) {
    if (!dir) return say('no worktree selected')
    if (!rm.usable(removals[dir])) return // no new process in a worktree being deleted
    try {
      const p = await api.NewTab(dir, kind)
      await refresh() // so no older snapshot can drop the tab after it's placed
      setLayout(dir, (l) => place(l, p, i ?? l.focus))
      focusNow()
    } catch (e) {
      say(errText(e), true)
    }
  }

  // closeTab stops a tab's process, asking first if that interrupts something.
  async function closeTab(dir: string, id: string) {
    const p = all.find((w) => w.dir === dir)?.panes?.find((p) => p.id === id)
    if (!p) return
    if (await api.TabBusy(id)) {
      const why = p.kind === 'claude' ? 'Claude is in the middle of a task.' : 'A process is still running in it.'
      if (!(await confirm(`Close tab ${layouts[dir]?.labels[id] ?? p.name}?`, `${why} Closing the tab stops it.`))) return say('cancelled')
    }
    try {
      await api.CloseTab(id)
      focusNow()
    } catch (e) {
      if (errText(e) !== `no pane ${id}`) say(errText(e), true) // else it exited meanwhile: closed already
    }
  }

  // change applies a layout change to the selected worktree, then focuses its
  // focused tab.
  function change(f: (l: Layout) => Layout) {
    if (!sel?.open) return
    setLayout(sel.dir, f)
    focusNow()
  }

  // splitRight moves the focused tab into a new right pane; with a single tab
  // there, a new shell opens in the right pane instead.
  function splitRight(dir = sel?.dir) {
    const l = dir ? layouts[dir] : undefined
    if (!dir || !l) return say('open a worktree first')
    if (!split(l)) return newTab(dir, 'shell', 1)
    setLayout(dir, (l) => split(l) ?? l)
    focusNow()
  }

  function cycleWs(d: number) {
    const open = all.filter((w) => w.open && rm.usable(removals[w.dir]))
    if (open.length === 0) return say('no worktrees are open')
    const i = open.findIndex((w) => w.dir === selected)
    const next = i < 0 ? open[d > 0 ? 0 : open.length - 1] : open[(i + d + open.length) % open.length]
    openWs(next.dir)
  }

  const openRepo = () => api.OpenRepo().catch((e) => say(errText(e), true))
  const addRepo = () => api.AddRepo().then((name) => name && say(`added ${name}`), (e) => say(errText(e), true))

  // The repo header menu's actions.
  const newIn = (path: string) => !busy && setModal({ kind: 'new', repo: path })
  const reveal = (path: string) => api.Reveal(path).catch((e) => say(errText(e), true))
  function toggleRepo(path: string) {
    const next = collapsed.includes(path) ? collapsed.filter((p) => p !== path) : [...collapsed, path]
    setCollapsed(next)
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify(next))
  }

  // A worktree going with the repo (from its config) that is mid-removal holds up
  // taking the repo out of the window, so the removal's outcome still has a row
  // to show in.
  const removingIn = (path: string) => {
    const config = repos.find((r) => r.path === path)?.config
    return listed.find((w) => w.config === config && rm.inFlight(removals[w.dir]))
  }

  // askRemoveRepo swaps an added repo's sidebar group for an inline confirm.
  function askRemoveRepo(path: string) {
    const w = removingIn(path)
    if (w) return say(`wait for ${w.name} to finish removing`)
    setFilter('') // so the group, and with it the confirm, is shown
    setRepoConfirm({ path, back: document.activeElement })
  }

  // answerRemoveRepo takes the repo out of the window (its worktrees and files
  // stay on disk; ⌘O adds it back), or doesn't, and ends the confirm.
  async function answerRemoveRepo(remove: boolean) {
    if (!repoConfirm || (remove && busy)) return // the status bar shows what's running
    const { path, back } = repoConfirm
    const r = repos.find((r) => r.path === path)
    const w = removingIn(path)
    if (remove && w) return say(`wait for ${w.name} to finish removing`)
    let done = false
    if (remove && r) {
      done = !!(await run(`removing ${r.name} from this window`, async () => {
        await api.RemoveRepo(r.path)
        await refresh() // so its group goes with the confirm, rather than after
        return true
      }))
      if (done) say(`removed ${r.name} from this window`)
    }
    setRepoConfirm(null)
    // Focus goes to the worktree list once the repo has gone (↩ reopens your
    // worktree, if it's still there), else back where it was, else to the
    // repo's ⋯.
    requestAnimationFrame(() => {
      if (done) return focusList()
      if (back instanceof HTMLElement && back.isConnected && back !== document.body) return back.focus()
      document.querySelector<HTMLElement>(`[data-repo="${CSS.escape(path)}"]`)?.focus()
    })
  }

  function setFont(d: number) {
    setFontDelta(d)
    localStorage.setItem(FONT_KEY, String(d))
  }

  function resizeSidebar(w: number) {
    const v = sidebarWidth(w)
    setSidebarW(v)
    localStorage.setItem(SIDEBAR_KEY, String(v))
  }

  function dispatch(action: string) {
    if (busy) return
    if (modal) {
      if (action === 'help' && modal.kind === 'help') setModal(null)
      return
    }
    const ws = target()
    const need = (f: (w: main.WorkspaceInfo) => unknown) => {
      if (!ws) return say('no worktree selected')
      if (rm.usable(removals[ws.dir])) return f(ws) // else its row says what's happening
    }
    switch (action) {
      case 'add-repo':
        return addRepo()
      case 'open-repo':
        return openRepo()
      case 'remove-repo':
        return need((w) => {
          const r = repos.find((r) => r.path === w.repoPath)
          if (!r) return say(`${w.name} is not in a repository`)
          if (!r.added) return say(`${r.name} is this window's own repository, so it can't be removed`)
          askRemoveRepo(r.path)
        })
      case 'new':
      case 'checkout':
        if (!snap?.canCreate) return say('no [[repo]] configured to create into')
        // Create in the targeted worktree's repo; the form can switch it.
        return setModal({ kind: action, repo: ws?.repoPath || repos[0].path })
      case 'terminal':
        return need((w) => api.OpenInTerminal(w.dir).then(() => say(`opened ${w.name} in terminal`), (e) => say(errText(e), true)))
      case 'close':
        return need(closeWs)
      case 'delete':
        return need(deleteWs)
      case 'reload':
        return reload(true)
      case 'goto':
        return focusList()
      case 'next-ws':
        return cycleWs(1)
      case 'prev-ws':
        return cycleWs(-1)
      case 'new-claude':
        return newTab(sel?.dir, 'claude')
      case 'new-shell':
        return newTab(sel?.dir, 'shell')
      case 'next-tab':
        return change((l) => cycle(l, 1))
      case 'prev-tab':
        return change((l) => cycle(l, -1))
      case 'split':
        return splitRight()
      case 'pane-left':
        return change((l) => focusPane(l, 0))
      case 'pane-right':
        return change((l) => focusPane(l, 1))
      case 'zoom':
        return change(toggleZoom)
      case 'font-up':
        return setFont(fontDelta + 1)
      case 'font-down':
        return setFont(fontDelta - 1)
      case 'font-reset':
        return setFont(0)
      case 'help':
        return setModal({ kind: 'help' })
    }
    if (action.startsWith('ws:')) {
      // ⌘1-9 number the list's rows, so one hidden in a collapsed group has no
      // number shown and doesn't open.
      const w = all[Number(action.slice(3))]
      if (w && w.repoPath === repoConfirm?.path) return say(`cancel removing ${w.repo} to open ${w.name}`)
      if (w && collapsed.includes(w.repoPath) && !filtered.some((f) => f.dir === w.dir)) return say(`expand ${w.repo} to open ${w.name}`)
      if (w) openWs(w.dir)
    }
  }

  // Backend events are subscribed once; they call the latest render's handlers.
  const live = useRef({ dispatch, openWs, reload, refresh, busy, modal, selTab })
  live.current = { dispatch, openWs, reload, refresh, busy, modal, selTab }
  useEffect(() => {
    const offs = [
      EventsOn('changed', () => live.current.refresh()),
      EventsOn('menu', (a: string) => live.current.dispatch(a)),
      EventsOn('open', (dir: string) => live.current.openWs(dir)),
    ]
    // Rescan when the window regains focus, so worktrees made elsewhere appear;
    // the tab in front of you has now been seen.
    const onFocus = () => {
      if (live.current.selTab) api.SeenTab(live.current.selTab)
      if (!live.current.busy && !live.current.modal) live.current.reload(false)
    }
    window.addEventListener('focus', onFocus)
    // ⌘⌫ is also an editing key (delete to line start; a DEL byte in xterm), so
    // inputs and panes consume it before the menu sees it. Claim it first. Mac
    // only: GTK runs menu accelerators before the page, so Linux needs no help.
    const onKey = (e: KeyboardEvent) => {
      const del = e.key === 'Backspace' && e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey
      if (del && navigator.userAgent.includes('Mac') && !live.current.modal) {
        e.preventDefault()
        e.stopPropagation()
        live.current.dispatch('delete')
      }
    }
    window.addEventListener('keydown', onKey, true)
    fetchSnap(api.State).then(async () => {
      const initial = await api.TakeInitial()
      if (initial) live.current.openWs(initial)
      else requestAnimationFrame(() => filterRef.current?.focus())
    })
    return () => {
      offs.forEach((off) => off())
      window.removeEventListener('focus', onFocus)
      window.removeEventListener('keydown', onKey, true)
    }
  }, [])

  // When a dialog closes, hand focus back to the pane (or the list) unless the
  // flow already moved it, so keys never go nowhere.
  const hadModal = useRef(false)
  useEffect(() => {
    if (hadModal.current && !modal) {
      requestAnimationFrame(() => {
        if (document.activeElement && document.activeElement !== document.body) return
        if (sel?.open) focusNow()
        else filterRef.current?.focus()
      })
    }
    hadModal.current = !!modal
  }, [modal])

  // Keyboard focus follows the focused tab. An action that asks (focusNow) always
  // puts it there, from the worktree filter too. A tab that comes to the front by
  // itself (its neighbour exited) takes it only from the page or the worktree's
  // own area, never from the sidebar, a menu or a dialog.
  useEffect(() => {
    if (selTab) focusTerm(selTab)
  }, [focusReq])
  useEffect(() => {
    const a = document.activeElement
    if (selTab && (!a || a === document.body || (a.closest('.workspace') && !a.closest('.menu')))) focusTerm(selTab)
  }, [selTab])

  // Closing a worktree's last tab takes focus with its terminal: give it to the
  // empty state's first button.
  const emptyRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!sel?.open && document.activeElement === document.body) emptyRef.current?.focus()
  }, [sel?.dir, sel?.open])

  // The tab in front of you has been seen: clear its "Claude wants you" mark
  // when it comes to the front, or when a mark arrives while it is there.
  const selMark = sel?.panes?.find((p) => p.id === selTab)?.claude
  useEffect(() => {
    if (selTab && (selMark === 'idle' || selMark === 'waiting') && document.hasFocus()) api.SeenTab(selTab)
  }, [selTab, selMark])

  // Forget a selection whose worktree is gone. Its terminals went with it, so
  // keys go to the worktree list rather than nowhere.
  useEffect(() => {
    if (selected && snap && !all.some((w) => w.dir === selected)) {
      setSelected(null)
      if (!modal && document.activeElement === document.body) focusList()
    }
  }, [snap])

  // The list cursor follows its row when rows above it come or go (a removed
  // row collapsing, a repo's group), so ↩ and ⌘⌫ act on the row you picked;
  // else it is clamped.
  const cursorRow = useRef<string | undefined>(undefined)
  useEffect(() => {
    cursorRow.current = filtered[cursor]?.dir
  })
  useLayoutEffect(() => {
    const i = filtered.findIndex((w) => w.dir === cursorRow.current)
    if (i >= 0) setCursor(i)
  }, [listed.map((w) => w.dir).join('\n'), collapsed.join('\n'), repoConfirm?.path])
  useEffect(() => setCursor((c) => Math.min(c, Math.max(0, filtered.length - 1))), [filtered.length])

  // Run removal rows' timers: "Removed." collapsing away, a kept branch's wait.
  useEffect(() => {
    const d = rm.nextDeadline(removals)
    if (d === null) return
    const t = setTimeout(() => setRemovals((rs) => rm.tick(rs, Math.max(Date.now(), d))), d - Date.now())
    return () => clearTimeout(t)
  }, [removals])

  // Title the window after the repo, so it's findable among other windows.
  useEffect(() => {
    const repo = sel?.repo || all[0]?.repo
    WindowSetTitle(repo ? `grove - ${repo}` : 'grove')
  }, [sel?.repo, all[0]?.repo])

  if (snap?.error) {
    return (
      <div className="fatal">
        <h1>No repository open</h1>
        <p>Pick a repo with a <code>.grove.toml</code> (create one with <code>grove init</code>), or run <code>grove</code> inside a repo.</p>
        <button className="primary" autoFocus onClick={openRepo}>
          Open Repository… <kbd>{key(snap, 'Add Repository')}</kbd>
        </button>
        <pre>{snap.error}</pre>
      </div>
    )
  }

  const onFilterKey = (e: React.KeyboardEvent) => {
    const down = e.key === 'ArrowDown' || (e.ctrlKey && (e.key === 'n' || e.key === 'j'))
    const up = e.key === 'ArrowUp' || (e.ctrlKey && (e.key === 'p' || e.key === 'k'))
    if (down || up) {
      e.preventDefault()
      setCursor((c) => Math.min(Math.max(c + (down ? 1 : -1), 0), Math.max(0, filtered.length - 1)))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const w = filtered[cursor]
      if (w && rm.usable(removals[w.dir])) openWs(w.dir)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      leaveList()
    }
  }

  const openList = all.filter((w) => w.open)

  return (
    // min() keeps the sidebar to half the window if the window shrinks later.
    <div className="app" style={{ '--sidebar': `min(${sidebarW}px, 50vw)` } as React.CSSProperties}>
      <Sidebar
        snap={snap}
        all={all}
        groups={grouped}
        filtered={filtered}
        selected={selected}
        cursor={cursor}
        listFocused={listFocused}
        filter={filter}
        filterRef={filterRef}
        onFilterChange={(v) => {
          setFilter(v)
          setCursor(0)
          setRepoConfirm(null) // it could be filtered out of sight
        }}
        onFilterFocus={() => {
          setListFocused(true)
          setCursor(Math.max(0, filtered.findIndex((w) => w.dir === selected)))
        }}
        onFilterBlur={() => setListFocused(false)}
        onFilterKey={onFilterKey}
        onOpen={openWs}
        removals={removals}
        onRemoval={onRemoval}
        onHold={(dir, held) => setRemovals((rs) => rm.hold(rs, dir, held, Date.now()))}
        collapsed={collapsed}
        onNewIn={newIn}
        onToggle={toggleRepo}
        onReveal={reveal}
        onAskRemove={askRemoveRepo}
        confirm={repoConfirm?.path ?? null}
        onConfirm={answerRemoveRepo}
        width={sidebarW}
        onResize={resizeSidebar}
      />

      <main className="main">
        {openList.map(
          (w) =>
            layouts[w.dir] && (
              <WorkspaceView
                key={w.dir}
                ws={w}
                layout={layouts[w.dir]}
                visible={w.dir === selected}
                font={font}
                snap={snap}
                onLayout={(f, focus = true) => {
                  setLayout(w.dir, f)
                  if (focus) focusNow()
                }}
                onRatio={(ratio, save) => {
                  setLayout(w.dir, (l) => ({ ...l, ratio }))
                  if (save) localStorage.setItem(SPLIT_KEY + w.dir, String(ratio))
                }}
                onNewTab={(kind, i) => newTab(w.dir, kind, i)}
                onCloseTab={(id) => closeTab(w.dir, id)}
                onSplit={() => splitRight(w.dir)}
              />
            ),
        )}
        {!sel?.open && (
          <div className="placeholder">
            {sel ? (
              <>
                <WorkspaceHeader ws={sel} />
                <p>No sessions open in this worktree.</p>
                <div className="actions">
                  <button ref={emptyRef} onClick={() => newTab(sel.dir, 'claude')}>
                    New Claude tab <kbd>{key(snap, 'New Claude Tab')}</kbd>
                  </button>
                  <button onClick={() => newTab(sel.dir, 'shell')}>
                    New shell <kbd>{key(snap, 'New Shell')}</kbd>
                  </button>
                </div>
              </>
            ) : (
              <>
                <p>Pick a worktree to open.</p>
                <p className="hint">
                  <kbd>{key(snap, 'Go to Worktree')}</kbd> go to · <kbd>{key(snap, 'New Worktree')}</kbd> new ·{' '}
                  <kbd>{key(snap, 'Keyboard Shortcuts')}</kbd> all shortcuts
                </p>
              </>
            )}
          </div>
        )}
      </main>

      <footer className="status">
        <span className={msg.err ? 'msg err' : 'msg'}>
          {busy ? (
            <>
              <span className="spinner" />
              {busy}…
            </>
          ) : (
            msg.text
          )}
        </span>
        <span className="hints">
          {key(snap, 'Go to Worktree')} go to · {key(snap, 'New Worktree')} new · {key(snap, 'New Claude Tab')} claude · {key(snap, 'New Shell')} shell ·{' '}
          {key(snap, 'Split Right')} split · {key(snap, 'Keyboard Shortcuts')} shortcuts
        </span>
      </footer>

      {modal?.kind === 'confirm' && <ConfirmDialog m={modal} close={() => setModal(null)} />}
      {(modal?.kind === 'new' || modal?.kind === 'checkout') && (
        <WorktreeForm
          kind={modal.kind}
          repos={repos}
          repo={modal.repo}
          busy={!!busy}
          run={run}
          onCancel={() => {
            setModal(null)
            leaveList()
          }}
          onDone={(dir) => {
            setModal(null)
            say(`created ${dir.split('/').pop()}`)
            openWs(dir)
          }}
        />
      )}
      {modal?.kind === 'help' && <Help snap={snap} close={() => setModal(null)} />}
    </div>
  )
}

// keepFocus returns focus to a dialog when something outside it takes focus
// (a pane mounting, a placeholder button), so its keys keep working.
const keepFocus = (e: React.FocusEvent<HTMLElement>) => {
  const el = e.currentTarget
  if (el.contains(e.relatedTarget as Node | null)) return
  requestAnimationFrame(() => {
    if (el.isConnected && !el.contains(document.activeElement)) (el.querySelector('input') ?? el).focus()
  })
}

function ConfirmDialog({ m, close }: { m: Confirm; close: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => ref.current?.focus(), [])
  const answer = (yes: boolean) => {
    close()
    m.resolve(yes)
  }
  const onKey = (e: React.KeyboardEvent) => {
    const k = e.key.toLowerCase()
    // Enter confirms unless the action is destructive, which needs an explicit Y.
    if (k === 'y' || (k === 'enter' && !m.danger && e.target === e.currentTarget)) {
      e.preventDefault()
      answer(true)
    } else if (k === 'n' || k === 'escape') {
      e.preventDefault()
      answer(false)
    }
  }
  return (
    <div className="overlay">
      <div className="dialog" role="dialog" tabIndex={-1} ref={ref} onKeyDown={onKey} onBlur={keepFocus}>
        <h2>{m.title}</h2>
        {m.detail && <p>{m.detail}</p>}
        <div className="buttons">
          <button onClick={() => answer(false)}>
            No <kbd>N</kbd>
          </button>
          <button className={m.danger ? 'danger' : 'primary'} onClick={() => answer(true)}>
            Yes <kbd>{m.danger ? 'Y' : 'Y / ↩'}</kbd>
          </button>
        </div>
      </div>
    </div>
  )
}

function WorktreeForm(props: {
  kind: 'new' | 'checkout'
  repos: main.RepoInfo[]
  repo: string // repo path to create in, preselected
  busy: boolean
  run: <T>(label: string, fn: () => Promise<T>, onErr?: (e: string) => void) => Promise<T | undefined>
  onDone: (dir: string) => void
  onCancel: () => void
}) {
  const { kind, repos, busy, run, onDone, onCancel } = props
  const formRef = useRef<HTMLFormElement>(null)
  const [repo, setRepo] = useState(props.repo)
  const [intention, setIntention] = useState('')
  const [branch, setBranch] = useState('')
  const [base, setBase] = useState('')
  const [branches, setBranches] = useState<string[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    api.Branches(repo).then((b) => setBranches(b ?? []))
    if (kind === 'new') api.DefaultBase(repo).then(setBase)
  }, [kind, repo])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setErr('')
    const label = kind === 'new' ? `creating ${intention.trim()}` : `checking out ${branch.trim()}`
    const dir = await run(label, () => (kind === 'new' ? api.Create(repo, intention, branch, base) : api.Checkout(repo, branch, intention)), setErr)
    if (dir) onDone(dir)
    else requestAnimationFrame(() => formRef.current?.querySelector('input')?.focus()) // fix and retry
  }

  const nameField = (
    <label key="name">
      <span>{kind === 'new' ? 'Intention' : 'Name'}</span>
      <input autoFocus={kind === 'new'} value={intention} onChange={(e) => setIntention(e.target.value)} placeholder="worktree name" spellCheck={false} />
    </label>
  )
  const branchField = (
    <label key="branch">
      <span>Branch</span>
      <input
        autoFocus={kind === 'checkout'}
        value={branch}
        onChange={(e) => setBranch(e.target.value)}
        placeholder={kind === 'new' ? 'new branch name' : 'existing branch'}
        list={kind === 'checkout' ? 'grove-branches' : undefined}
        spellCheck={false}
      />
    </label>
  )
  return (
    <div className="overlay">
      <form
        className="dialog form"
        role="dialog"
        tabIndex={-1}
        ref={formRef}
        onBlur={keepFocus}
        onSubmit={submit}
        onKeyDown={(e) => e.key === 'Escape' && !busy && (e.preventDefault(), onCancel())}
      >
        <h2>{kind === 'new' ? 'New worktree' : 'New worktree from branch'}</h2>
        {/* Disabled while the git operation runs, so it can't be re-triggered or cancelled mid-flight. */}
        <fieldset disabled={busy}>
          {repos.length > 1 && (
            <label>
              <span>Repository</span>
              <select value={repo} onChange={(e) => setRepo(e.target.value)}>
                {repos.map((r) => (
                  <option key={r.path} value={r.path}>
                    {r.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {kind === 'new' ? [nameField, branchField] : [branchField, nameField]}
          {kind === 'new' && (
            <label>
              <span>Base</span>
              <input value={base} onChange={(e) => setBase(e.target.value)} placeholder="base branch" list="grove-branches" spellCheck={false} />
            </label>
          )}
          <datalist id="grove-branches">
            {branches.map((b) => (
              <option key={b} value={b} />
            ))}
          </datalist>
          {err && <p className="err">{err}</p>}
          <div className="buttons">
            <button type="button" onClick={onCancel}>
              Cancel <kbd>esc</kbd>
            </button>
            <button type="submit" className="primary">
              {busy ? 'Creating…' : 'Create'} <kbd>↩</kbd>
            </button>
          </div>
        </fieldset>
      </form>
    </div>
  )
}

function Help({ snap, close }: { snap: main.Snapshot | null; close: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => ref.current?.focus(), [])
  const groups = new Map<string, main.ShortcutInfo[]>()
  for (const s of snap?.shortcuts ?? []) groups.set(s.group, [...(groups.get(s.group) ?? []), s])
  const mac = navigator.userAgent.includes('Mac')
  groups.set('Everywhere else', [
    { group: '', label: 'Newline in a pane (Claude Code)', keys: mac ? '⇧↩' : 'Shift+Enter' },
    { group: '', label: 'Worktree list: move / open / back', keys: '↑↓ ↩ esc' },
    { group: '', label: 'Dialogs: next field / confirm / cancel', keys: '⇥ ↩ esc' },
    { group: '', label: 'Open a link in a pane', keys: mac ? '⌘-click' : 'Ctrl-click' },
    { group: '', label: 'Select text over a mouse-aware app', keys: mac ? '⌥-drag' : 'Shift-drag' },
  ])
  return (
    <div className="overlay" onClick={close}>
      <div className="dialog help" role="dialog" tabIndex={-1} ref={ref} onBlur={keepFocus} onKeyDown={(e) => e.key === 'Escape' && close()} onClick={(e) => e.stopPropagation()}>
        <h2>Keyboard shortcuts</h2>
        <div className="groups">
          {[...groups].map(([g, list]) => (
            <section key={g}>
              <h3>{g}</h3>
              {list.map((s) => (
                <div className="row" key={s.label}>
                  <span>{s.label}</span>
                  <kbd>{s.keys}</kbd>
                </div>
              ))}
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}
