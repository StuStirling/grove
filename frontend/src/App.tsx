import { useEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime'
import type { main } from '../wailsjs/go/models'
import { WorkspaceView, terms } from './Panes'

type Confirm = { kind: 'confirm'; title: string; detail?: string; danger?: boolean; resolve: (yes: boolean) => void }
type Modal = { kind: 'new' } | { kind: 'checkout' } | { kind: 'help' } | Confirm

const FONT_KEY = 'grove.fontDelta'
const MONO = 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Monaco, monospace'
const errText = (e: unknown) => String((e as Error)?.message ?? e)

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
  const [focusIdx, setFocusIdx] = useState<Record<string, number>>({})
  const [zoom, setZoom] = useState<Record<string, number | null>>({})
  const [filter, setFilter] = useState('')
  const [cursor, setCursor] = useState(0)
  const [listFocused, setListFocused] = useState(false)
  const [modal, setModal] = useState<Modal | null>(null)
  const [busy, setBusy] = useState('')
  const [msg, setMsg] = useState<{ text: string; err?: boolean }>({ text: '' })
  const [fontDelta, setFontDelta] = useState(() => Number(localStorage.getItem(FONT_KEY)) || 0)
  const filterRef = useRef<HTMLInputElement>(null)
  const lastRight = useRef<Record<string, number>>({})

  const all = snap?.workspaces ?? []
  const q = filter.trim().toLowerCase()
  const filtered = q ? all.filter((w) => w.name.toLowerCase().includes(q) || w.branch.toLowerCase().includes(q)) : all
  const sel = all.find((w) => w.name === selected)
  const font = {
    family: snap?.fontFamily || MONO,
    size: Math.min(32, Math.max(8, (snap?.fontSize || 13) + fontDelta)),
  }

  const say = (text: string, err = false) => setMsg({ text, err })
  const refresh = () => api.State().then(setSnap)
  const reload = async (manual: boolean) => {
    setSnap(await api.Reload())
    if (manual) say('refreshed')
  }

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

  function focusPane(ws: string, i: number, panes = all.find((w) => w.name === ws)?.panes ?? []) {
    const p = panes[Math.min(i, panes.length - 1)]
    if (!p) return
    setFocusIdx((f) => ({ ...f, [ws]: Math.min(i, panes.length - 1) }))
    focusTerm(p.id)
  }

  // openWs starts a workspace's panes if needed and switches to it.
  async function openWs(name: string) {
    try {
      const panes = await api.Open(name)
      setSelected(name)
      setFilter('')
      setListFocused(false)
      filterRef.current?.blur()
      await refresh()
      focusPane(name, focusIdx[name] ?? 0, panes)
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
    if (sel?.open) focusPane(sel.name, focusIdx[sel.name] ?? 0)
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

  async function deleteWs(ws: main.WorkspaceInfo) {
    if (!ws.repoPath) return say(`${ws.name} is not a git worktree`)
    if (!(await confirm(`Delete worktree ${ws.name}?`, `Removes ${ws.dir} and stops its panes.`, true))) return say('cancelled')
    let r = await run(`removing ${ws.name}`, () => api.Remove(ws.name, false))
    if (r === undefined) return
    if (r === 'dirty') {
      if (!(await confirm(`${ws.name} has uncommitted changes`, 'Force delete? The changes will be lost.', true))) return say('cancelled')
      r = await run(`removing ${ws.name}`, () => api.Remove(ws.name, true))
      if (r === undefined) return
    }
    let done = `removed ${ws.name}`
    let failed = false
    if (ws.branch && (await confirm(`Removed ${ws.name}`, `Also delete branch ${ws.branch}?`))) {
      const b = await run(`deleting branch ${ws.branch}`, () => api.DeleteBranch(ws.repoPath, ws.branch))
      if (b === 'unmerged') done = `removed ${ws.name}, branch ${ws.branch} unmerged, kept`
      else if (b === '') done = `removed ${ws.name} and branch ${ws.branch}`
      else failed = true // error already shown
    }
    await reload(false)
    setFilter('')
    if (!failed) say(done)
  }

  async function addShell() {
    if (!sel?.open) return say('open a worktree first')
    try {
      const p = await api.AddShell(sel.name)
      await refresh()
      focusPane(sel.name, (sel.panes?.length ?? 0), [...(sel.panes ?? []), p])
    } catch (e) {
      say(errText(e), true)
    }
  }

  function cycleWs(d: number) {
    const open = all.filter((w) => w.open)
    if (open.length === 0) return say('no worktrees are open')
    const i = open.findIndex((w) => w.name === selected)
    const next = i < 0 ? open[d > 0 ? 0 : open.length - 1] : open[(i + d + open.length) % open.length]
    openWs(next.name)
  }

  function movePane(dir: 'left' | 'right' | 'up' | 'down' | 'next' | 'prev') {
    if (!sel?.open) return
    const n = sel.panes?.length ?? 0
    const i = Math.min(focusIdx[sel.name] ?? 0, n - 1)
    let j = i
    if (dir === 'next') j = (i + 1) % n
    else if (dir === 'prev') j = (i - 1 + n) % n
    else if (dir === 'left' && i > 0) j = 0
    else if (dir === 'right' && i === 0 && n > 1) j = Math.min(lastRight.current[sel.name] ?? 1, n - 1)
    else if (dir === 'up' && i > 1) j = i - 1
    else if (dir === 'down' && i >= 1 && i < n - 1) j = i + 1
    if (j > 0) lastRight.current[sel.name] = j
    if (zoom[sel.name] != null) setZoom((z) => ({ ...z, [sel.name]: j }))
    focusPane(sel.name, j)
  }

  function toggleZoom() {
    if (!sel?.open) return
    const i = focusIdx[sel.name] ?? 0
    setZoom((z) => ({ ...z, [sel.name]: z[sel.name] === i ? null : i }))
    focusPane(sel.name, i)
  }

  const openRepo = () => api.OpenRepo().catch((e) => say(errText(e), true))

  function setFont(d: number) {
    setFontDelta(d)
    localStorage.setItem(FONT_KEY, String(d))
  }

  function dispatch(action: string) {
    if (busy) return
    if (modal) {
      if (action === 'help' && modal.kind === 'help') setModal(null)
      return
    }
    const ws = target()
    const need = (f: (w: main.WorkspaceInfo) => unknown) => (ws ? f(ws) : say('no worktree selected'))
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
      case 'next-pane':
        return movePane('next')
      case 'prev-pane':
        return movePane('prev')
      case 'pane-left':
      case 'pane-right':
      case 'pane-up':
      case 'pane-down':
        return movePane(action.slice(5) as 'left')
      case 'add-shell':
        return addShell()
      case 'zoom':
        return toggleZoom()
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
  const live = useRef({ dispatch, openWs, reload, refresh, busy, modal })
  live.current = { dispatch, openWs, reload, refresh, busy, modal }
  useEffect(() => {
    const offs = [
      EventsOn('changed', () => live.current.refresh()),
      EventsOn('menu', (a: string) => live.current.dispatch(a)),
      EventsOn('open', (name: string) => live.current.openWs(name)),
    ]
    // Rescan when the window regains focus, so worktrees made elsewhere appear.
    const onFocus = () => !live.current.busy && !live.current.modal && live.current.reload(false)
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
    api.State().then(async (s) => {
      setSnap(s)
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
        if (sel?.open) focusPane(sel.name, focusIdx[sel.name] ?? 0)
        else filterRef.current?.focus()
      })
    }
    hadModal.current = !!modal
  }, [modal])

  // Forget a selection whose worktree is gone; clamp stale pane indices.
  useEffect(() => {
    if (selected && snap && !all.some((w) => w.name === selected)) setSelected(null)
  }, [snap])
  useEffect(() => setCursor((c) => Math.min(c, Math.max(0, filtered.length - 1))), [filtered.length])

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
      if (w) openWs(w.name)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      leaveList()
    }
  }

  const openList = all.filter((w) => w.open)

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          grove<span>{all[0]?.repo}</span>
        </div>
        <input
          ref={filterRef}
          className="filter"
          placeholder={`Go to worktree  ${key(snap, 'Go to Worktree')}`}
          value={filter}
          spellCheck={false}
          onChange={(e) => {
            setFilter(e.target.value)
            setCursor(0)
          }}
          onFocus={() => {
            setListFocused(true)
            setCursor(Math.max(0, filtered.findIndex((w) => w.name === selected)))
          }}
          onBlur={() => setListFocused(false)}
          onKeyDown={onFilterKey}
        />
        <ul className="list">
          {filtered.map((w, i) => (
            <li
              key={w.name}
              className={(w.name === selected ? 'sel ' : '') + (listFocused && i === cursor ? 'cursor' : '')}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => openWs(w.name)}
              title={w.dir}
            >
              <span className="gutter">
                <Status w={w} active={w.name === selected} />
                <Claude state={w.claude} />
              </span>
              <span className="text">
                <span className="name">{w.name}</span>
                {w.branch && <span className="branch">{w.branch}</span>}
              </span>
              {all.indexOf(w) < 9 && <span className="num">{key(snap, 'Worktree 1–9').replace('1–9', String(all.indexOf(w) + 1))}</span>}
            </li>
          ))}
          {filtered.length === 0 && <li className="empty">{all.length ? 'no match' : 'no worktrees'}</li>}
        </ul>
        <div className="legend">
          <span><i className="st-active">●</i> active</span>
          <span><i className="st-open">○</i> open</span>
          <span><i className="cl-waiting">◆</i><i className="cl-idle">◆</i> claude wants you</span>
          <span><i className="cl-working">◌</i> busy</span>
        </div>
      </aside>

      <main className="main">
        {openList.map((w) => (
          <WorkspaceView
            key={w.name}
            ws={w}
            visible={w.name === selected}
            focused={Math.min(focusIdx[w.name] ?? 0, (w.panes?.length ?? 1) - 1)}
            zoomed={zoom[w.name] != null && zoom[w.name]! < (w.panes?.length ?? 0) ? zoom[w.name]! : null}
            font={font}
            onFocus={(i) => setFocusIdx((f) => (f[w.name] === i ? f : { ...f, [w.name]: i }))}
          />
        ))}
        {!sel?.open && (
          <div className="placeholder">
            {sel ? (
              <>
                <p>
                  <b>{sel.name}</b> is not running.
                </p>
                <button onClick={() => openWs(sel.name)}>
                  Open <kbd>↩</kbd>
                </button>
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
          {key(snap, 'Go to Worktree')} go to · {key(snap, 'New Worktree')} new · {key(snap, 'Keyboard Shortcuts')} shortcuts
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

// key returns a shortcut's label (e.g. "⌘P") from the backend's table.
function key(snap: main.Snapshot | null, label: string) {
  return snap?.shortcuts?.find((s) => s.label === label)?.keys ?? ''
}

function Status({ w, active }: { w: main.WorkspaceInfo; active: boolean }) {
  if (!w.open) return <i> </i>
  return active ? <i className="st-active" title="active">●</i> : <i className="st-open" title="open, running in the background">○</i>
}

function Claude({ state }: { state: string }) {
  switch (state) {
    case 'waiting':
      return <i className="cl-waiting" title="Claude is waiting for permission">◆</i>
    case 'idle':
      return <i className="cl-idle" title="Claude finished: your turn">◆</i>
    case 'working':
      return <i className="cl-working" title="Claude is working">◌</i>
  }
  return <i> </i>
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
