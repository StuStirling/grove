import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime'
import type { main } from '../wailsjs/go/models'
import { WorkspaceHeader, WorkspaceView, terms } from './Panes'
import { Sidebar, sidebarWidth, type RemovalAction } from './Sidebar'
import * as rm from './removal'
import { cycle, focusPane, focusedTab, place, split, sync, toggleZoom, type Layout } from './layout'
import { errText, key } from './util'

type Confirm = { kind: 'confirm'; title: string; detail?: string; danger?: boolean; resolve: (yes: boolean) => void }
type Modal = { kind: 'new' } | { kind: 'checkout' } | { kind: 'help' } | Confirm

const FONT_KEY = 'grove.fontDelta'
const SIDEBAR_KEY = 'grove.sidebarWidth'
const SPLIT_KEY = 'grove.split:' // + worktree name: its split ratio
const MONO = 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Monaco, monospace'

const savedRatio = (ws: string) => {
  const r = Number(localStorage.getItem(SPLIT_KEY + ws))
  return r > 0 && r < 1 ? r : 0.5
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
  const filterRef = useRef<HTMLInputElement>(null)
  const snapSeq = useRef({ asked: 0, shown: 0 })

  const all = snap?.workspaces ?? []
  const q = filter.trim().toLowerCase()
  // Rows being removed stay in the list, where they were, until they're done.
  const listed = rm.withRemovals(all, removals)
  const filtered = q ? listed.filter((w) => w.name.toLowerCase().includes(q) || w.branch.toLowerCase().includes(q)) : listed
  const sel = all.find((w) => w.name === selected)
  const selTab = focusedTab(sel?.open ? layouts[sel.name] : undefined)
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
      for (const w of s.workspaces ?? []) if (w.open) next[w.name] = sync(ls[w.name], w.panes ?? [], savedRatio(w.name))
      return next
    })
    setRemovals((rs) => rm.sync(rs, s.workspaces ?? []))
  }
  const refresh = () => fetchSnap(api.State)
  const reload = async (manual: boolean) => {
    await fetchSnap(api.Reload)
    if (manual) say('refreshed')
  }
  const setLayout = (ws: string, f: (l: Layout) => Layout) => setLayouts((ls) => (ls[ws] ? { ...ls, [ws]: f(ls[ws]) } : ls))
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

  // openWs starts a workspace's panes if needed and switches to it.
  async function openWs(name: string) {
    // Not a worktree being removed. A gone row's name is a new worktree's, which
    // may open (just made) before its snapshot drops the row.
    if (!rm.usable(removals[name]) && !removals[name].gone) return
    try {
      await api.Open(name)
      setSelected(name)
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
    const i = filtered.findIndex((w) => w.name === selected)
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
      await api.Close(ws.name)
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
    await removeRow(ws.name, false)
  }

  async function removeRow(name: string, force: boolean) {
    let res: main.RemoveResult
    try {
      res = await api.Remove(name, force)
    } catch (e) {
      res = { status: 'failed', reason: errText(e), detail: errText(e), branchKept: '', branchDetail: '' }
    }
    setRemovals((rs) => rm.result(rs, name, res, Date.now()))
    if (res.status === 'removed') await reload(false) // drop it from menus; its row stays until done
  }

  async function onRemoval(r: rm.Removal, action: RemovalAction) {
    const name = r.ws.name
    switch (action) {
      case 'keep':
        return setRemovals((rs) => rm.keep(rs, name))
      case 'dismiss':
        return setRemovals((rs) => rm.dismiss(rs, name))
      case 'force':
        if (!(await confirm(`Force remove ${name}?`, 'Its uncommitted changes will be lost.', true))) return
        setRemovals((rs) => rm.force(rs, name))
        return removeRow(name, true)
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
        setRemovals((rs) => rm.branchResult(rs, name, res, Date.now()))
      }
    }
  }

  // newTab opens a Claude or shell tab in a worktree (opening the worktree with
  // just that tab if it isn't open) and shows it in pane i, else the focused one.
  async function newTab(ws: string | undefined, kind: 'claude' | 'shell', i?: number) {
    if (!ws) return say('no worktree selected')
    if (!rm.usable(removals[ws])) return // no new process in a worktree being deleted
    try {
      const p = await api.NewTab(ws, kind)
      await refresh() // so no older snapshot can drop the tab after it's placed
      setLayout(ws, (l) => place(l, p, i ?? l.focus))
      focusNow()
    } catch (e) {
      say(errText(e), true)
    }
  }

  // closeTab stops a tab's process, asking first if that interrupts something.
  async function closeTab(ws: string, id: string) {
    const p = all.find((w) => w.name === ws)?.panes?.find((p) => p.id === id)
    if (!p) return
    if (await api.TabBusy(id)) {
      const why = p.kind === 'claude' ? 'Claude is in the middle of a task.' : 'A process is still running in it.'
      if (!(await confirm(`Close tab ${layouts[ws]?.labels[id] ?? p.name}?`, `${why} Closing the tab stops it.`))) return say('cancelled')
    }
    try {
      await api.CloseTab(id)
      focusNow()
    } catch (e) {
      say(errText(e), true)
    }
  }

  // change applies a layout change to the selected worktree, then focuses its
  // focused tab.
  function change(f: (l: Layout) => Layout) {
    if (!sel?.open) return
    setLayout(sel.name, f)
    focusNow()
  }

  // splitRight moves the focused tab into a new right pane; with a single tab
  // there, a new shell opens in the right pane instead.
  function splitRight(ws = sel?.name) {
    const l = ws ? layouts[ws] : undefined
    if (!ws || !l) return say('open a worktree first')
    if (!split(l)) return newTab(ws, 'shell', 1)
    setLayout(ws, (l) => split(l) ?? l)
    focusNow()
  }

  function cycleWs(d: number) {
    const open = all.filter((w) => w.open && rm.usable(removals[w.name]))
    if (open.length === 0) return say('no worktrees are open')
    const i = open.findIndex((w) => w.name === selected)
    const next = i < 0 ? open[d > 0 ? 0 : open.length - 1] : open[(i + d + open.length) % open.length]
    openWs(next.name)
  }

  const openRepo = () => api.OpenRepo().catch((e) => say(errText(e), true))

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
      if (rm.usable(removals[ws.name])) return f(ws) // else its row says what's happening
    }
    switch (action) {
      case 'open-repo':
        return openRepo()
      case 'new':
      case 'checkout':
        if (!snap?.canCreate) return say('no [[repo]] configured to create into')
        return setModal({ kind: action })
      case 'terminal':
        return need((w) => api.OpenInTerminal(w.name).then(() => say(`opened ${w.name} in terminal`), (e) => say(errText(e), true)))
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
        return newTab(sel?.name, 'claude')
      case 'new-shell':
        return newTab(sel?.name, 'shell')
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
      const w = all[Number(action.slice(3))]
      if (w) openWs(w.name)
    }
  }

  // Backend events are subscribed once; they call the latest render's handlers.
  const live = useRef({ dispatch, openWs, reload, refresh, busy, modal, selTab })
  live.current = { dispatch, openWs, reload, refresh, busy, modal, selTab }
  useEffect(() => {
    const offs = [
      EventsOn('changed', () => live.current.refresh()),
      EventsOn('menu', (a: string) => live.current.dispatch(a)),
      EventsOn('open', (name: string) => live.current.openWs(name)),
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

  // Keyboard focus follows the focused tab: when another tab comes to the front
  // (picked, opened, or its neighbour closed) or an action asks (focusNow), its
  // terminal takes focus, unless you're typing in the worktree filter.
  useEffect(() => {
    if (selTab && document.activeElement !== filterRef.current) focusTerm(selTab)
  }, [selTab, focusReq])

  // The tab in front of you has been seen: clear its "Claude wants you" mark
  // when it comes to the front, or when a mark arrives while it is there.
  const selMark = sel?.panes?.find((p) => p.id === selTab)?.claude
  useEffect(() => {
    if (selTab && (selMark === 'idle' || selMark === 'waiting') && document.hasFocus()) api.SeenTab(selTab)
  }, [selTab, selMark])

  // Forget a selection whose worktree is gone. Its terminals went with it, so
  // keys go to the worktree list rather than nowhere.
  useEffect(() => {
    if (selected && snap && !all.some((w) => w.name === selected)) {
      setSelected(null)
      if (!modal && document.activeElement === document.body) focusList()
    }
  }, [snap])

  // The list cursor follows its row when rows above it come or go (a removed
  // row collapsing), so ↩ and ⌘⌫ act on the row you picked; else it is clamped.
  const cursorRow = useRef<string | undefined>(undefined)
  useEffect(() => {
    cursorRow.current = filtered[cursor]?.name
  })
  useLayoutEffect(() => {
    const i = filtered.findIndex((w) => w.name === cursorRow.current)
    if (i >= 0) setCursor(i)
  }, [listed.map((w) => w.name).join('\n')])
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
          Open Repository… <kbd>{key(snap, 'Open Repository')}</kbd>
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
      if (w && rm.usable(removals[w.name])) openWs(w.name)
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
        filtered={filtered}
        selected={selected}
        cursor={cursor}
        listFocused={listFocused}
        filter={filter}
        filterRef={filterRef}
        onFilterChange={(v) => {
          setFilter(v)
          setCursor(0)
        }}
        onFilterFocus={() => {
          setListFocused(true)
          setCursor(Math.max(0, filtered.findIndex((w) => w.name === selected)))
        }}
        onFilterBlur={() => setListFocused(false)}
        onFilterKey={onFilterKey}
        onOpen={openWs}
        removals={removals}
        onRemoval={onRemoval}
        onHold={(name, held) => setRemovals((rs) => rm.hold(rs, name, held, Date.now()))}
        width={sidebarW}
        onResize={resizeSidebar}
      />

      <main className="main">
        {openList.map(
          (w) =>
            layouts[w.name] && (
              <WorkspaceView
                key={w.name}
                ws={w}
                layout={layouts[w.name]}
                visible={w.name === selected}
                font={font}
                snap={snap}
                onLayout={(f, focus = true) => {
                  setLayout(w.name, f)
                  if (focus) focusNow()
                }}
                onRatio={(ratio, save) => {
                  setLayout(w.name, (l) => ({ ...l, ratio }))
                  if (save) localStorage.setItem(SPLIT_KEY + w.name, String(ratio))
                }}
                onNewTab={(kind, i) => newTab(w.name, kind, i)}
                onCloseTab={(id) => closeTab(w.name, id)}
                onSplit={() => splitRight(w.name)}
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
                  <button onClick={() => newTab(sel.name, 'claude')}>
                    New Claude tab <kbd>{key(snap, 'New Claude Tab')}</kbd>
                  </button>
                  <button onClick={() => newTab(sel.name, 'shell')}>
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
          busy={!!busy}
          run={run}
          onCancel={() => {
            setModal(null)
            leaveList()
          }}
          onDone={(name) => {
            setModal(null)
            say(`created ${name}`)
            openWs(name)
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
  busy: boolean
  run: <T>(label: string, fn: () => Promise<T>, onErr?: (e: string) => void) => Promise<T | undefined>
  onDone: (name: string) => void
  onCancel: () => void
}) {
  const { kind, busy, run, onDone, onCancel } = props
  const formRef = useRef<HTMLFormElement>(null)
  const [intention, setIntention] = useState('')
  const [branch, setBranch] = useState('')
  const [base, setBase] = useState('')
  const [branches, setBranches] = useState<string[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    api.Branches().then((b) => setBranches(b ?? []))
    if (kind === 'new') api.DefaultBase().then(setBase)
  }, [kind])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    setErr('')
    const label = kind === 'new' ? `creating ${intention.trim()}` : `checking out ${branch.trim()}`
    const name = await run(label, () => (kind === 'new' ? api.Create(intention, branch, base) : api.Checkout(branch, intention)), setErr)
    if (name) onDone(name)
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
