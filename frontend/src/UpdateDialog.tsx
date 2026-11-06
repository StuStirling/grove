import { useEffect, useRef, useState } from 'react'
import * as api from '../wailsjs/go/main/App'
import type { main } from '../wailsjs/go/models'
import { errText, keepFocus } from './util'
import { doneLine, keyAction, prLine, pushLabel, pushQuestion, stepText, type Action } from './update'

const mac = navigator.userAgent.includes('Mac')

// What the dialog shows: checking, the plan, a push question, a stopped
// rebase/merge, or a single line with Close (refusal, up to date, failure).
type View =
  | { kind: 'checking' }
  | { kind: 'plan'; plan: main.UpdatePlan }
  | { kind: 'push'; plan: main.UpdatePlan; push: string; done: string }
  | { kind: 'conflicts'; plan: main.UpdatePlan | null; progress: main.InProgress }
  | { kind: 'note'; text: string; detail?: string; err?: boolean }

// Update brings a worktree's branch up to date with its base: rebase or merge
// as its PR's review state says, conflicts handed back or to Claude, then the
// push it needs. Keyboard first: every button shows its key.
export function Update({ ws, onClose, onClaude, say }: { ws: main.WorkspaceInfo; onClose: () => void; onClaude: () => void; say: (text: string) => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [view, setView] = useState<View>({ kind: 'checking' })
  const [strategy, setStrategy] = useState('')
  const [running, setRunning] = useState(-1) // step index in flight; -2: some other command
  const [error, setError] = useState<{ reason: string; detail: string } | null>(null)
  const busy = running !== -1

  useEffect(() => ref.current?.focus(), [view.kind])
  useEffect(() => void plan(), [])

  async function plan() {
    setView({ kind: 'checking' })
    setError(null)
    try {
      show(await api.PlanUpdate(ws.dir))
    } catch (e) {
      setView({ kind: 'note', text: errText(e), err: true })
    }
  }

  function show(p: main.UpdatePlan) {
    if (p.progress) return setView({ kind: 'conflicts', plan: p, progress: p.progress })
    if (p.refusal) return setView({ kind: 'note', text: p.refusal })
    setStrategy(p.strategy)
    if (p.steps?.length) return setView({ kind: 'plan', plan: p })
    if (p.push) return setView({ kind: 'push', plan: p, push: p.push, done: `Up to date with ${p.base}.` })
    setView({ kind: 'note', text: `Up to date with ${p.base}.` })
  }

  // after handles a run or continue result; "done" from a continue re-plans,
  // as later steps haven't run.
  function after(r: main.UpdateResult, p: main.UpdatePlan | null, replan: boolean) {
    if (r.status === 'stopped' && r.progress) return setView({ kind: 'conflicts', plan: p, progress: r.progress })
    if (r.status === 'failed') return setError({ reason: r.reason, detail: r.detail })
    if (replan || !p) return void plan()
    const done = doneLine(strategy, p.steps)
    if (r.push) return setView({ kind: 'push', plan: p, push: r.push, done })
    say(done)
    onClose()
  }

  async function act(a: Action) {
    if (busy) return
    if (a === 'cancel') return onClose()
    setError(null)
    try {
      if (view.kind === 'plan') {
        if (a === 'rebase' || a === 'merge') return setStrategy(a)
        if (a !== 'update' || !strategy) return
        setRunning(0)
        after(await api.RunUpdate(ws.dir, view.plan.head, strategy), view.plan, false)
      } else if (view.kind === 'push' && a === 'push') {
        setRunning(-2)
        await api.PushUpdate(ws.dir, view.push)
        say(`Pushed ${view.plan.branch}`)
        onClose()
      } else if (view.kind === 'conflicts') {
        if (a === 'claude') return onClaude()
        setRunning(-2)
        if (a === 'continue') after(await api.ContinueUpdate(ws.dir), view.plan, true)
        if (a === 'abort') {
          await api.AbortUpdate(ws.dir)
          await plan()
        }
      }
    } catch (e) {
      setError({ reason: errText(e), detail: '' })
    } finally {
      setRunning(-1)
    }
  }

  const onKey = (e: React.KeyboardEvent) => {
    if (view.kind === 'checking' || view.kind === 'note') {
      if (e.key === 'Escape' || e.key === 'Enter') (e.preventDefault(), onClose())
      return
    }
    const a = keyAction(view.kind, e, mac)
    if (!a) return
    e.preventDefault()
    e.stopPropagation()
    void act(a)
  }

  const remote = (p: main.UpdatePlan) => p.base.split('/')[0]
  const opName = (p: main.InProgress) => (p.kind === 'merge' ? 'Merge' : 'Rebase')

  return (
    <div className="overlay">
      <div className="dialog update" role="dialog" aria-label={`Update ${ws.branch}`} aria-busy={busy || undefined} tabIndex={-1} ref={ref} onKeyDown={onKey} onBlur={keepFocus}>
        <h2>Update {ws.branch || ws.name}</h2>

        {view.kind === 'checking' && <p className="muted">Checking…</p>}

        {view.kind === 'note' && (
          <>
            <p className={view.err ? 'err' : undefined} title={view.detail || undefined}>
              {view.text}
            </p>
            <div className="buttons">
              <button className="primary" onClick={onClose}>
                Close <kbd>esc</kbd>
              </button>
            </div>
          </>
        )}

        {view.kind === 'plan' && (
          <>
            <p className="muted">{prLine(view.plan.pr)}</p>
            {view.plan.warnings?.map((w) => (
              <p key={w} className="warn">
                ⚠ {w}
              </p>
            ))}
            <div className="strategy" role="radiogroup" aria-label="Strategy">
              Strategy
              {(['rebase', 'merge'] as const).map((s) => (
                <label key={s}>
                  <input type="radio" name="strategy" checked={strategy === s} disabled={busy} onChange={() => setStrategy(s)} />
                  {s === 'rebase' ? 'Rebase' : 'Merge'} <kbd>{s === 'rebase' ? 'R' : 'M'}</kbd>
                </label>
              ))}
            </div>
            <ol className="steps">
              {view.plan.steps.map((s, i) => (
                <li key={i} className={running === i ? 'running' : undefined}>
                  {stepText(s, strategy)}
                </li>
              ))}
            </ol>
            <div className="buttons">
              <button onClick={() => act('cancel')} disabled={busy}>
                Cancel <kbd>esc</kbd>
              </button>
              <button className="primary" onClick={() => act('update')} disabled={busy || !strategy}>
                {busy ? 'Updating…' : 'Update'} <kbd>↩</kbd>
              </button>
            </div>
          </>
        )}

        {view.kind === 'push' && (
          <>
            <p>✓ {view.done}</p>
            <p>{pushQuestion(view.push, view.plan.pr, remote(view.plan))}</p>
            <div className="buttons">
              <button onClick={() => act('cancel')} disabled={busy}>
                Not now <kbd>esc</kbd>
              </button>
              <button className={view.push === 'force' ? 'danger' : 'primary'} onClick={() => act('push')} disabled={busy}>
                {busy ? 'Pushing…' : pushLabel(view.push)} <kbd>↩</kbd>
              </button>
            </div>
          </>
        )}

        {view.kind === 'conflicts' && (
          <>
            <p className="warn">
              ⚠ {opName(view.progress)} stopped {view.progress.files?.length ? 'on conflicts' : 'with no conflicted files listed'}
            </p>
            {!!view.progress.files?.length && (
              <ul className="files">
                {view.progress.files.map((f) => (
                  <li key={f}>{f}</li>
                ))}
              </ul>
            )}
            <div className="buttons">
              <button className="danger" onClick={() => act('abort')} disabled={busy}>
                Abort <kbd>{mac ? '⌘⌫' : 'Ctrl+⌫'}</kbd>
              </button>
              <button onClick={() => act('claude')} disabled={busy}>
                Ask Claude <kbd>C</kbd>
              </button>
              <button className="primary" onClick={() => act('continue')} disabled={busy}>
                {busy ? 'Working…' : 'Continue'} <kbd>↩</kbd>
              </button>
            </div>
          </>
        )}

        {error && (
          <p className="err" title={error.detail || undefined}>
            {error.reason}
          </p>
        )}
      </div>
    </div>
  )
}
