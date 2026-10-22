import { Fragment, useEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import type { main } from '../wailsjs/go/models'
import {
  buildPrompt,
  clashes,
  defaultRepos,
  existingWorktree,
  latest,
  LINEAR_LIMITED,
  LINEAR_NOT_CONNECTED,
  LINEAR_REJECTED,
  nextLead,
  REPOS_KEY,
  suggestBranch,
  suggestName,
  type Target,
  type Ticket,
} from './start'
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

const DONE = ['completed', 'canceled']
const ACTION: Record<string, string> = { create: 'new branch', checkout: 'existing branch', reuse: 'existing worktree' }

const savedRepos = (): string[] => {
  try {
    const v = JSON.parse(localStorage.getItem(REPOS_KEY) ?? '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

const openLink = (url: string) => (e: React.MouseEvent) => {
  e.preventDefault()
  api.OpenURL(url)
}

type Row = { state: 'running' | 'done' | 'failed' | 'skipped'; dir?: string; err?: string }

// StartWork picks the repos, lead, branch and worktree name for a ticket, then
// creates the worktrees: the other repos first, in parallel, then the lead, so
// its seed only adds worktrees that exist.
export function StartWork(props: { snap: main.Snapshot; ticket: Ticket; current: string; onDone: (leadDir: string, created: number) => void; onCancel: () => void }) {
  const { snap, ticket, onDone, onCancel } = props
  const repos = snap.repos ?? []
  const nameOf = (p: string) => repos.find((r) => r.path === p)?.name ?? p
  const [ticked, setTicked] = useState(() => defaultRepos(savedRepos(), repos, props.current))
  const [lead, setLead] = useState(() => nextLead(props.current, ticked))
  const [branch, setBranch] = useState(() => suggestBranch(ticket))
  const [name, setName] = useState(() => suggestName(ticket))
  const [seed, setSeed] = useState(true)
  const [showPrompt, setShowPrompt] = useState(false)
  const [plans, setPlans] = useState<Record<string, main.RepoPlan>>({})
  const [planning, setPlanning] = useState(true)
  const [err, setErr] = useState('')
  const [phase, setPhase] = useState<'form' | 'creating'>('form')
  const [rows, setRows] = useState<Record<string, Row>>({})
  const seq = useRef(latest()).current

  // Plan each ticked repo once the fields settle, so conflicts show before anything is created.
  useEffect(() => {
    const id = seq.next()
    setPlanning(true)
    const t = setTimeout(
      () =>
        Promise.all(ticked.map((p) => api.PlanRepo(p, branch, name).then((pl) => [p, pl] as const))).then(
          (ps) => {
            if (!seq.current(id)) return
            setPlans(Object.fromEntries(ps))
            setErr('')
            setPlanning(false)
          },
          (e) => {
            if (!seq.current(id)) return
            setErr(errText(e))
            setPlanning(false)
          },
        ),
      250,
    )
    return () => clearTimeout(t)
  }, [ticked, branch, name])

  const others = ticked.filter((p) => p !== lead)
  const targets: Target[] = (lead ? [lead, ...others] : []).map((p) => ({ repo: nameOf(p), path: plans[p]?.path ?? '' }))
  const clash = clashes(targets)
  const problem = (p: string) => (plans[p]?.action === 'conflict' ? plans[p].reason : clash[nameOf(p)])
  const blocked = ticked.length === 0 || planning || ticked.some(problem)

  function toggle(p: string) {
    const next = repos.map((r) => r.path).filter((x) => (x === p ? !ticked.includes(p) : ticked.includes(x)))
    setTicked(next)
    setLead((l) => nextLead(l, next))
  }

  const runRepo = (p: string) => {
    setRows((r) => ({ ...r, [p]: { state: 'running' } }))
    api.StartRepo(p, branch, name, { prompt: '', addDirs: [] }).then(
      (dir) => setRows((r) => ({ ...r, [p]: { state: 'done', dir } })),
      (e) => setRows((r) => ({ ...r, [p]: { state: 'failed', err: errText(e) } })),
    )
  }

  // The lead starts once every other repo is done or skipped.
  useEffect(() => {
    if (phase !== 'creating' || rows[lead] || !others.every((p) => rows[p]?.state === 'done' || rows[p]?.state === 'skipped')) return
    const done = others.filter((p) => rows[p]?.state === 'done')
    const leadTarget = { repo: nameOf(lead), path: plans[lead]?.path ?? '' }
    const s = seed
      ? {
          prompt: buildPrompt(ticket, [leadTarget, ...done.map((p) => ({ repo: nameOf(p), path: rows[p].dir ?? '' }))], branch),
          addDirs: done.map((p) => rows[p].dir ?? ''),
        }
      : { prompt: '', addDirs: [] }
    setRows((r) => ({ ...r, [lead]: { state: 'running' } }))
    api.StartRepo(lead, branch, name, s).then(
      (dir) => onDone(dir, done.length + 1),
      (e) => setRows((r) => ({ ...r, [lead]: { state: 'failed', err: errText(e) } })),
    )
  }, [phase, rows])

  function create() {
    if (blocked) return
    localStorage.setItem(REPOS_KEY, JSON.stringify(ticked))
    setPhase('creating')
    others.forEach(runRepo)
  }

  // Retrying the lead clears its row, and the effect above starts it again.
  const retry = (p: string) =>
    p === lead
      ? setRows((r) => {
          const rest = { ...r }
          delete rest[p]
          return rest
        })
      : runRepo(p)

  const n = ticked.length
  const prompt = buildPrompt(ticket, targets, branch)
  const command = ['claude --permission-mode plan', ...others.map((p) => '--add-dir ' + (plans[p]?.path ?? ''))].join(' \\\n  ')

  return (
    <div className="overlay">
      <form
        className="dialog form start"
        role="dialog"
        tabIndex={-1}
        onBlur={keepFocus}
        onSubmit={(e) => {
          e.preventDefault()
          if (phase === 'form') create()
        }}
        onKeyDown={(e) => e.key === 'Escape' && (e.preventDefault(), onCancel())}
      >
        <h2>{ticket.kind === 'issue' ? `Start work on ${ticket.issue.id}` : 'Start work on a project'}</h2>
        <TicketCard ticket={ticket} />
        {phase === 'form' ? (
          <>
            <div className="repos-head">
              <span>Repositories in this window</span>
              <span>lead</span>
            </div>
            <div className="repos">
              {repos.map((r) => {
                const on = ticked.includes(r.path)
                const plan = plans[r.path]
                const why = on && problem(r.path)
                return (
                  <div key={r.path} className="repo-row">
                    <div className="repo-line">
                      <label>
                        <input type="checkbox" checked={on} onChange={() => toggle(r.path)} />
                        <b>{r.name}</b>
                        <span className="detail">{on && plan && plan.action !== 'conflict' ? `${ACTION[plan.action]} → ${plan.path}` : ''}</span>
                      </label>
                      <input type="radio" name="lead" aria-label={`Make ${r.name} the lead`} checked={on && r.path === lead} disabled={!on} onChange={() => setLead(r.path)} />
                    </div>
                    {why && <p className="err">{why}</p>}
                  </div>
                )
              })}
            </div>
            <div className="cols">
              <label>
                <span>
                  Branch <small>{ticket.kind === 'issue' ? 'from Linear' : 'suggested from the project name'}</small>
                </span>
                <input value={branch} onChange={(e) => setBranch(e.target.value)} spellCheck={false} />
              </label>
              <label className="name">
                <span>Worktree name</span>
                <input value={name} onChange={(e) => setName(e.target.value)} spellCheck={false} />
              </label>
            </div>
            {lead && (
              <label className="check">
                <input type="checkbox" checked={seed} onChange={() => setSeed(!seed)} />
                <span>
                  Start Claude in plan mode in {nameOf(lead)}
                  {n > 1 && ` with the other ${n === 2 ? 'worktree' : `${n - 1} worktrees`} added`}
                </span>
              </label>
            )}
            {lead && seed && (
              <div className="prompt">
                <button type="button" className="link" onClick={() => setShowPrompt(!showPrompt)} aria-expanded={showPrompt}>
                  {showPrompt ? '▾ Hide the prompt' : '▸ Show the prompt'}
                </button>
                {showPrompt && <pre>{command + " \\\n  '<the prompt>'\n\n" + prompt}</pre>}
              </div>
            )}
            {err && <p className="err">{err}</p>}
            <div className="buttons">
              <button type="button" onClick={onCancel}>
                Back <kbd>esc</kbd>
              </button>
              <button type="submit" className="primary" disabled={blocked}>
                {n === 1 ? 'Create worktree' : `Create ${n} worktrees`} <kbd>↩</kbd>
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="repos">
              {[...others, lead].map((p) => {
                const row = rows[p]
                const detail = !row
                  ? p === lead
                    ? 'waiting for the others'
                    : ''
                  : row.state === 'running'
                    ? 'creating…'
                    : row.state === 'skipped'
                      ? 'skipped'
                      : row.state === 'done'
                        ? row.dir
                        : row.err
                return (
                  <div key={p} className={'repo-row' + (row?.state === 'failed' ? ' failed' : '')}>
                    <div className="repo-line">
                      {!row || row.state === 'running' ? (
                        <span className="spinner" aria-hidden="true" />
                      ) : (
                        <i className={'mark ' + row.state}>{row.state === 'done' ? '✓' : row.state === 'failed' ? '✕' : '–'}</i>
                      )}
                      <b>{nameOf(p)}</b>
                      <span className="detail">
                        {detail}
                        {p === lead && ' · lead'}
                      </span>
                    </div>
                    {row?.state === 'failed' && (
                      <span className="rm-actions">
                        <button type="button" onClick={() => retry(p)}>
                          Retry
                        </button>
                        {p !== lead && (
                          <button type="button" onClick={() => setRows((r) => ({ ...r, [p]: { state: 'skipped' } }))}>
                            Skip {nameOf(p)}
                          </button>
                        )}
                      </span>
                    )}
                  </div>
                )
              })}
            </div>
            <div className="buttons">
              <span className="hint">Worktrees already created stay if you close.</span>
              <button type="button" onClick={onCancel}>
                Close <kbd>esc</kbd>
              </button>
            </div>
          </>
        )}
      </form>
    </div>
  )
}

function TicketCard({ ticket }: { ticket: Ticket }) {
  if (ticket.kind === 'issue') {
    const i = ticket.issue
    return (
      <div className="ticket">
        <div className="ticket-meta">
          <StateGlyph type={i.stateType} />
          <span>{[i.id, i.state, i.project].filter(Boolean).join(' · ')}</span>
          <a href={i.url} onClick={openLink(i.url)}>
            Open in Linear ↗
          </a>
        </div>
        <div className="ticket-title">{i.title}</div>
        {i.description && <p className="ticket-desc">{i.description}</p>}
      </div>
    )
  }
  const p = ticket.project
  const issues = p.issues ?? []
  return (
    <div className="ticket">
      <div className="ticket-meta">
        <ProjectGlyph />
        <span>{['Project', p.status, `${Math.round(p.progress * 100)}% done`, p.targetDate && `target ${shortDate(p.targetDate)}`].filter(Boolean).join(' · ')}</span>
        <a href={p.url} onClick={openLink(p.url)}>
          Open in Linear ↗
        </a>
      </div>
      <div className="ticket-title">{p.name}</div>
      {p.description && <p className="ticket-desc">{p.description}</p>}
      {issues.length > 0 && (
        <div className="ticket-issues">
          <span className="hint">All {issues.length} issues go into the plan, done ones marked so Claude doesn't plan them again</span>
          {issues.map((i) => (
            <div key={i.id} className={DONE.includes(i.stateType) ? 'done' : ''}>
              <span className="id">{i.id}</span>
              <span className="title">{i.title}</span>
              <span>{i.state}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
