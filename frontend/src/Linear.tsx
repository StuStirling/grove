import { Fragment, useEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import type { main } from '../wailsjs/go/models'
import { existingWorktree, latest, LINEAR_LIMITED, LINEAR_NOT_CONNECTED, LINEAR_REJECTED, type Ticket } from './start'
import { errText, keepFocus } from './util'

const SECURITY_URL = 'https://linear.app/settings/account/security'
const MOD = navigator.userAgent.includes('Mac') ? '⌘' : 'Ctrl+'

// ConnectDialog asks for a personal API key; the backend checks it with Linear
// before saving it to the keychain.
export function ConnectDialog(props: { rejected: boolean; onDone: () => void; onCancel: () => void }) {
  const { onDone, onCancel } = props
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState(props.rejected ? 'Linear rejected the saved key. Paste a new one.' : '')

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy || !key.trim()) return
    setBusy(true)
    setErr('')
    try {
      await api.LinearConnect(key)
      onDone()
    } catch (e) {
      setErr(errText(e) === LINEAR_REJECTED ? "Linear didn't accept that key." : errText(e))
      setBusy(false)
    }
  }

  return (
    <div className="overlay">
      <form
        className="dialog form"
        role="dialog"
        tabIndex={-1}
        onBlur={keepFocus}
        onSubmit={submit}
        onKeyDown={(e) => e.key === 'Escape' && !busy && (e.preventDefault(), onCancel())}
      >
        <h2>Connect Linear</h2>
        <p>
          grove searches your Linear issues and projects with a personal API key. It's kept in your keychain, never in <code>.grove.toml</code>, and only
          used to read.
        </p>
        <ol className="steps">
          <li>
            In Linear, open <b>Settings ▸ Account ▸ Security &amp; access</b>.{' '}
            <a
              href={SECURITY_URL}
              onClick={(e) => {
                e.preventDefault()
                api.OpenURL(SECURITY_URL)
              }}
            >
              Open in browser ↗
            </a>
          </li>
          <li>
            Under <b>Personal API keys</b>, create a key with <b>Read</b> access and paste it here.
          </li>
        </ol>
        <fieldset disabled={busy}>
          <label>
            <span>API key</span>
            <input type="password" autoFocus value={key} onChange={(e) => setKey(e.target.value)} placeholder="lin_api_…" spellCheck={false} autoComplete="off" />
          </label>
          {err && <p className="err">{err}</p>}
          <div className="buttons">
            <button type="button" onClick={onCancel}>
              Cancel <kbd>esc</kbd>
            </button>
            <button type="submit" className="primary">
              {busy ? 'Checking…' : 'Connect'} <kbd>↩</kbd>
            </button>
          </div>
        </fieldset>
      </form>
    </div>
  )
}

// LinearAccountRow is the Linear section of Settings: whose key is saved.
export function LinearAccountRow({ onConnect }: { onConnect: () => void }) {
  const [acc, setAcc] = useState<{ connected: boolean; name: string } | null>(null)
  const [err, setErr] = useState('')
  const load = () =>
    api.LinearAccount().then(
      (a) => {
        setAcc(a)
        setErr('')
      },
      (e) => {
        setAcc({ connected: false, name: '' })
        setErr(errText(e))
      },
    )
  useEffect(() => {
    load()
  }, [])
  const disconnect = () => api.LinearDisconnect().then(load, (e) => setErr(errText(e)))

  const status = !acc
    ? 'Checking…'
    : acc.connected
      ? `Connected as ${acc.name || 'your Linear account'}`
      : err === LINEAR_REJECTED
        ? 'Linear rejected the saved key'
        : 'Not connected'
  return (
    <div className="field">
      <span>{status}</span>
      {acc &&
        (acc.connected ? (
          <button type="button" onClick={disconnect}>
            Disconnect
          </button>
        ) : (
          <button type="button" onClick={onConnect}>
            Connect…
          </button>
        ))}
      {err && err !== LINEAR_REJECTED && <p className="err">{err}</p>}
    </div>
  )
}

type Hit = { kind: 'issue'; issue: main.LinearIssue; worktree?: main.WorkspaceInfo } | { kind: 'project'; project: main.LinearProject }
type Scope = 'all' | 'issues' | 'projects'

const hitURL = (h: Hit) => (h.kind === 'issue' ? h.issue.url : h.project.url)
const shortDate = (d: string) => new Date(d + 'T00:00').toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })

// SearchPalette lists the viewer's work, or what a query finds, and starts work
// on a pick (or goes to the worktree an issue already has).
export function SearchPalette(props: {
  snap: main.Snapshot
  onPick: (t: Ticket) => void
  onGoTo: (dir: string) => void
  onNeedConnect: (rejected: boolean) => void
  onClose: () => void
}) {
  const { snap, onPick, onGoTo, onNeedConnect, onClose } = props
  const [query, setQuery] = useState('')
  const [scope, setScope] = useState<Scope>('all')
  const [results, setResults] = useState<main.LinearResults | null>(null)
  const [cursor, setCursor] = useState(0)
  const [loading, setLoading] = useState(true)
  const [picking, setPicking] = useState(false)
  const [err, setErr] = useState('')
  const [note, setNote] = useState('')
  const seq = useRef(latest()).current
  const listRef = useRef<HTMLDivElement>(null)

  const fail = (e: unknown) => {
    const m = errText(e)
    if (m === LINEAR_NOT_CONNECTED || m === LINEAR_REJECTED) onNeedConnect(m === LINEAR_REJECTED)
    else if (m === LINEAR_LIMITED) setNote(m) // keep what's listed
    else setErr(m)
  }

  // Search waits for a pause in typing: Linear allows 30 searches a minute.
  useEffect(() => {
    const id = seq.next()
    const term = query.trim()
    setLoading(true)
    const t = setTimeout(
      () =>
        api.LinearSearch(term).then(
          (r) => {
            if (!seq.current(id)) return
            setResults(r)
            setCursor(0)
            setErr('')
            setNote('')
            setLoading(false)
          },
          (e) => {
            if (!seq.current(id)) return
            setLoading(false)
            fail(e)
          },
        ),
      term ? 400 : 0,
    )
    return () => clearTimeout(t)
  }, [query])

  const typed = query.trim() !== ''
  const issues: Hit[] =
    scope === 'projects' ? [] : (results?.issues ?? []).map((issue) => ({ kind: 'issue', issue, worktree: existingWorktree(issue.branchName, snap.workspaces ?? []) }))
  const projects: Hit[] = scope === 'issues' ? [] : (results?.projects ?? []).map((project) => ({ kind: 'project', project }))
  const sections = [
    { title: typed ? 'Issues' : 'Assigned to you', hits: issues },
    { title: typed ? 'Projects' : 'Your projects', hits: projects },
  ].filter((s) => s.hits.length)
  const flat = [...issues, ...projects]
  const cur = flat[Math.min(cursor, flat.length - 1)]

  useEffect(() => listRef.current?.querySelector('.hit.cursor')?.scrollIntoView({ block: 'nearest' }), [cursor])

  async function act(h: Hit) {
    if (picking) return
    if (h.kind === 'issue' && h.worktree) return onGoTo(h.worktree.dir)
    setPicking(true)
    try {
      onPick(h.kind === 'issue' ? { kind: 'issue', issue: await api.GetLinearIssue(h.issue.id) } : { kind: 'project', project: await api.GetLinearProject(h.project.id) })
    } catch (e) {
      fail(e)
      setPicking(false)
    }
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      setCursor((c) => Math.max(0, Math.min(flat.length - 1, c + (e.key === 'ArrowDown' ? 1 : -1))))
    } else if (e.key === 'Enter' && cur) {
      e.preventDefault()
      if (e.metaKey || e.ctrlKey) api.OpenURL(hitURL(cur))
      else act(cur)
    }
  }

  return (
    <div className="overlay">
      <div
        className="dialog palette"
        role="dialog"
        aria-label="Search Linear"
        tabIndex={-1}
        onBlur={keepFocus}
        onKeyDown={(e) => e.key === 'Escape' && (e.preventDefault(), onClose())}
      >
        <div className="palette-head">
          <input
            className="q"
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={onKey}
            placeholder="Search Linear issues and projects"
            aria-label="Search Linear issues and projects"
            spellCheck={false}
          />
          <div className="chips">
            {(['all', 'issues', 'projects'] as Scope[]).map((s) => (
              <button
                key={s}
                type="button"
                aria-pressed={scope === s}
                onClick={() => {
                  setScope(s)
                  setCursor(0)
                }}
              >
                {s[0].toUpperCase() + s.slice(1)}
              </button>
            ))}
          </div>
        </div>
        <div className="hits" ref={listRef}>
          {sections.map((s) => (
            <Fragment key={s.title}>
              <h3>{s.title}</h3>
              {s.hits.map((h) => {
                const i = flat.indexOf(h)
                return (
                  <div
                    key={h.kind === 'issue' ? h.issue.id : h.project.id}
                    className={'hit' + (h === cur ? ' cursor' : '')}
                    onMouseEnter={() => setCursor(i)}
                    onClick={() => act(h)}
                  >
                    {h.kind === 'issue' ? (
                      <>
                        <StateGlyph type={h.issue.stateType} />
                        <span className="id">{h.issue.id}</span>
                        <span className="title">{h.issue.title}</span>
                        <span className={'meta' + (h.worktree ? ' wt' : '')}>{h.worktree ? `worktree in ${h.worktree.repo}` : h.issue.project || h.issue.team}</span>
                        <span className="state">{h.issue.state}</span>
                      </>
                    ) : (
                      <>
                        <ProjectGlyph />
                        <span className="title">{h.project.name}</span>
                        <span className="meta">
                          {Math.round(h.project.progress * 100)}% done{h.project.targetDate && ` · ${shortDate(h.project.targetDate)}`}
                        </span>
                        <span className="state">{h.project.status}</span>
                      </>
                    )}
                  </div>
                )
              })}
            </Fragment>
          ))}
          {err && <p className="err">{err}</p>}
          {!err && loading && !results && <p className="empty">Loading…</p>}
          {!err && !loading && results && flat.length === 0 && (
            <p className="empty">{typed ? `No issues or projects match “${query.trim()}”.` : 'Nothing assigned to you.'}</p>
          )}
        </div>
        <footer>
          <span>
            ↑↓ select · ↩ {cur?.kind === 'issue' && cur.worktree ? 'go to worktree' : 'start work'} · {MOD}↩ open in Linear · esc close
          </span>
          <span>{picking ? 'Opening…' : note}</span>
        </footer>
      </div>
    </div>
  )
}

// StateGlyph draws an issue's workflow state the way Linear does, roughly.
function StateGlyph({ type }: { type: string }) {
  const c = ({ started: 'var(--waiting)', completed: 'var(--idle)', canceled: 'var(--dim)' } as Record<string, string>)[type] ?? 'var(--muted)'
  return (
    <svg className="glyph" width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
      <circle
        cx="7"
        cy="7"
        r="5.5"
        fill={type === 'completed' ? c : 'none'}
        stroke={c}
        strokeWidth="1.5"
        strokeDasharray={type === 'backlog' || type === 'triage' ? '2 1.6' : undefined}
      />
      {type === 'started' && <path d="M7 3.5 A3.5 3.5 0 0 1 7 10.5 Z" fill={c} />}
      {type === 'canceled' && <path d="M4.5 9.5 L9.5 4.5" stroke={c} strokeWidth="1.5" />}
    </svg>
  )
}

function ProjectGlyph() {
  return (
    <svg className="glyph" width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
      <rect x="1.75" y="1.75" width="10.5" height="10.5" rx="2.5" fill="none" stroke="var(--idle)" strokeWidth="1.5" />
      <path d="M4.5 7 H9.5 M4.5 4.5 H9.5 M4.5 9.5 H7.5" stroke="var(--idle)" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  )
}
