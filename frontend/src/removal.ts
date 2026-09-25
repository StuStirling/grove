import type { main } from '../wailsjs/go/models'

// A removal is shown in the worktree's own sidebar row. Once removed cleanly the
// row says so for DONE_MS, then collapses over COLLAPSE_MS. A kept branch waits
// KEPT_MS for a decision; hovering or focusing the row pauses that wait.
export const DONE_MS = 2000
export const COLLAPSE_MS = 200
export const KEPT_MS = 10_000

type State =
  | { kind: 'removing' }
  | { kind: 'done'; until: number; collapsing: boolean } // until: when this phase ends
  | { kind: 'kept'; reason: string; detail: string; left: number; since: number | null } // since: null while paused
  | { kind: 'deleting' } // Delete branch in flight
  | { kind: 'failed'; reason: string; detail: string; dirty: boolean }

// The worktree and the rows above it are kept so its row stays put after a
// reload drops the worktree, until the row is dismissed or collapses. gone: a
// shown snapshot no longer lists the worktree.
export type Removal = State & { ws: main.WorkspaceInfo; above: string[]; gone: boolean }
export type Removals = Record<string, Removal>

const put = (rs: Removals, r: Removal, s: State): Removals => ({ ...rs, [r.ws.name]: { ws: r.ws, above: r.above, gone: r.gone, ...s } })
const drop = (rs: Removals, name: string): Removals => {
  const { [name]: _, ...rest } = rs
  return rest
}
const done = (now: number): State => ({ kind: 'done', until: now + DONE_MS, collapsing: false })
const kept = (reason: string, detail: string, now: number): State => ({ kind: 'kept', reason, detail, left: KEPT_MS, since: now })

// usable is true while the worktree is still there to open or act on: no removal
// or one that failed.
export const usable = (r?: Removal) => !r || r.kind === 'failed'

// start begins removing a worktree shown in listed. Its row remembers the names
// above it there; a retried removal keeps the place it had.
export function start(rs: Removals, ws: main.WorkspaceInfo, listed: main.WorkspaceInfo[]): Removals {
  const above = rs[ws.name]?.above ?? listed.slice(0, Math.max(0, listed.findIndex((w) => w.name === ws.name))).map((w) => w.name)
  return { ...rs, [ws.name]: { ws, above, gone: false, kind: 'removing' } }
}

// result applies Remove's answer. A row that is no longer removing (dismissed)
// ignores it.
export function result(rs: Removals, name: string, res: main.RemoveResult, now: number): Removals {
  const r = rs[name]
  if (r?.kind !== 'removing') return rs
  if (res.status === 'removed') return put(rs, r, res.branchKept ? kept(res.branchKept, res.branchDetail, now) : done(now))
  return put(rs, r, { kind: 'failed', reason: res.reason, detail: res.detail, dirty: res.status === 'dirty' })
}

export const force = (rs: Removals, name: string): Removals => (rs[name]?.kind === 'failed' ? put(rs, rs[name], { kind: 'removing' }) : rs)

// keep gives up on a failed removal: the worktree is still there, so its row goes
// back to normal.
export const keep = (rs: Removals, name: string): Removals => (rs[name]?.kind === 'failed' ? drop(rs, name) : rs)

export const dismiss = (rs: Removals, name: string): Removals => (rs[name]?.kind === 'kept' ? drop(rs, name) : rs)

// deleting starts Delete branch. It takes the entry the button belonged to, so a
// row whose timer ran out while the confirm dialog was up comes back to report.
export function deleting(rs: Removals, r: Removal): Removals {
  const cur = rs[r.ws.name] ?? r
  return cur.kind === 'kept' ? put(rs, cur, { kind: 'deleting' }) : rs
}

// branchResult applies DeleteBranch's answer: kept "" = deleted, else why it was kept.
export function branchResult(rs: Removals, name: string, res: main.BranchResult, now: number): Removals {
  const r = rs[name]
  if (r?.kind !== 'deleting') return rs
  return put(rs, r, res.kept ? kept(res.kept, res.detail, now) : done(now))
}

// hold pauses a kept row's timer while it is hovered or focused, and resumes it
// with the time that was left.
export function hold(rs: Removals, name: string, held: boolean, now: number): Removals {
  const r = rs[name]
  if (r?.kind !== 'kept' || held === (r.since === null)) return rs
  const left = r.since === null ? r.left : r.left - (now - r.since)
  return put(rs, r, { kind: 'kept', reason: r.reason, detail: r.detail, left, since: held ? null : now })
}

// deadline is when a row next changes by itself, or null.
export function deadline(r: Removal): number | null {
  if (r.kind === 'done') return r.until
  if (r.kind === 'kept' && r.since !== null) return r.since + r.left
  return null
}

export function nextDeadline(rs: Removals): number | null {
  const ds = Object.values(rs).map(deadline).filter((d) => d !== null)
  return ds.length ? Math.min(...ds) : null
}

// tick applies every timed change that is due by now.
export function tick(rs: Removals, now: number): Removals {
  let out = rs
  for (const r of Object.values(rs)) {
    const d = deadline(r)
    if (d === null || d > now) continue
    if (r.kind === 'done' && !r.collapsing) out = put(out, r, { kind: 'done', until: now + COLLAPSE_MS, collapsing: true })
    else out = drop(out, r.ws.name)
  }
  return out
}

// sync applies a snapshot now on screen. A removed row whose worktree it lacks
// is marked gone; a later snapshot listing that name again means a new worktree,
// so the row goes rather than lend it its state. (A snapshot from before the
// reload lists the old worktree, but can't drop an unmarked row.) A failed
// removal whose worktree went some other way goes too: its goal was reached.
export function sync(rs: Removals, list: main.WorkspaceInfo[]): Removals {
  const have = new Set(list.map((w) => w.name))
  let out = rs
  for (const r of Object.values(rs)) {
    const here = have.has(r.ws.name)
    if ((here && r.gone) || (!here && r.kind === 'failed')) out = drop(out, r.ws.name)
    else if (!here && !r.gone && r.kind !== 'removing') out = { ...out, [r.ws.name]: { ...r, gone: true } }
  }
  return out
}

// withRemovals puts rows whose worktree has gone from the list back where they
// were: under the nearest row above them that is still shown (perhaps another
// such row), else at the top.
export function withRemovals(list: main.WorkspaceInfo[], rs: Removals): main.WorkspaceInfo[] {
  const out = [...list]
  const have = new Set(list.map((w) => w.name))
  for (const r of Object.values(rs)) {
    if (have.has(r.ws.name)) continue
    const above = new Set(r.above)
    let at = 0
    out.forEach((w, i) => {
      if (above.has(w.name)) at = i + 1
    })
    out.splice(at, 0, r.ws)
  }
  return out
}
