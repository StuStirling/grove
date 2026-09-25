import type { main } from '../wailsjs/go/models'
import * as rm from './removal'
import { key } from './util'

export type RemovalAction = 'force' | 'keep' | 'delete-branch' | 'dismiss'

// Sidebar is the worktree list: a filter box (⌘P) over one row per worktree.
export function Sidebar(props: {
  snap: main.Snapshot | null
  all: main.WorkspaceInfo[]
  filtered: main.WorkspaceInfo[]
  selected: string | null
  cursor: number
  listFocused: boolean
  filter: string
  filterRef: React.RefObject<HTMLInputElement | null>
  onFilterChange: (v: string) => void
  onFilterFocus: () => void
  onFilterBlur: () => void
  onFilterKey: (e: React.KeyboardEvent) => void
  onOpen: (name: string) => void
  removals: rm.Removals
  onRemoval: (r: rm.Removal, action: RemovalAction) => void
  onHold: (name: string, held: boolean) => void
}) {
  const { snap, all, filtered, selected, cursor, listFocused, filter, filterRef, removals } = props

  // A row button acts without opening the row. Used from the keyboard, focus
  // moves to the filter when the button goes away with the row's state.
  const act = (r: rm.Removal, action: RemovalAction) => (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    if (document.activeElement === e.currentTarget && (action === 'keep' || action === 'dismiss')) filterRef.current?.focus()
    props.onRemoval(r, action)
  }

  return (
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
        onChange={(e) => props.onFilterChange(e.target.value)}
        onFocus={props.onFilterFocus}
        onBlur={props.onFilterBlur}
        onKeyDown={props.onFilterKey}
      />
      <ul className="list">
        {filtered.map((w, i) => {
          const r = removals[w.name]
          const n = all.indexOf(w)
          // A kept branch's row waits while it is hovered or focused.
          const hold = r?.kind === 'kept' ? {
            onMouseOver: () => props.onHold(w.name, true),
            onMouseLeave: (e: React.MouseEvent<HTMLElement>) => props.onHold(w.name, e.currentTarget.contains(document.activeElement)),
            onFocus: () => props.onHold(w.name, true),
            onBlur: (e: React.FocusEvent<HTMLElement>) =>
              props.onHold(w.name, e.currentTarget.matches(':hover') || e.currentTarget.contains(e.relatedTarget as Node | null)),
          } : {}
          const cls = [
            w.name === selected && 'sel',
            listFocused && i === cursor && 'cursor',
            r && 'rm-' + r.kind,
            r?.kind === 'done' && r.collapsing && 'collapsing',
          ]
          return (
            <li
              key={w.name}
              className={cls.filter(Boolean).join(' ')}
              aria-busy={r?.kind === 'removing' || r?.kind === 'deleting' || undefined}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => rm.usable(r) && props.onOpen(w.name)}
              title={w.dir}
              {...hold}
            >
              <span className="gutter">
                {r && !rm.usable(r) ? (
                  <RemovalMark r={r} />
                ) : (
                  <>
                    <Status w={w} active={w.name === selected} />
                    <Claude state={w.claude} />
                  </>
                )}
              </span>
              <span className="text">
                <span className="name">{w.name}</span>
                {w.branch && <span className="branch">{w.branch}</span>}
                {r?.kind === 'removing' && <span className="rm-line busy">Removing worktree…</span>}
                {r?.kind === 'deleting' && <span className="rm-line busy">Deleting branch…</span>}
                {r?.kind === 'done' && <span className="rm-line">Removed.</span>}
                {r?.kind === 'kept' && (
                  <>
                    <span className="rm-line">{r.reason === 'unmerged' ? 'Removed. Branch kept (unmerged).' : `Removed. Branch kept: ${r.reason}`}</span>
                    <span className="rm-actions">
                      <button onClick={act(r, 'delete-branch')} aria-label={`Delete branch ${w.branch} of ${w.name}`}>
                        Delete branch
                      </button>
                      <button onClick={act(r, 'dismiss')} aria-label={`Dismiss ${w.name}`}>
                        Dismiss
                      </button>
                    </span>
                  </>
                )}
                {r?.kind === 'failed' && (
                  <>
                    <span className="rm-line err" title={r.detail}>
                      Couldn't remove: {r.reason}
                    </span>
                    <span className="rm-actions">
                      {r.dirty && (
                        <button onClick={act(r, 'force')} aria-label={`Force remove ${w.name}`}>
                          Force remove
                        </button>
                      )}
                      <button onClick={act(r, 'keep')} aria-label={`Keep worktree ${w.name}`}>
                        Keep
                      </button>
                    </span>
                  </>
                )}
              </span>
              {rm.usable(r) && n >= 0 && n < 9 && <span className="num">{key(snap, 'Worktree 1–9').replace('1–9', String(n + 1))}</span>}
            </li>
          )
        })}
        {filtered.length === 0 && <li className="empty">{all.length ? 'no match' : 'no worktrees'}</li>}
      </ul>
      <div className="legend">
        <span><i className="st-active">●</i> active</span>
        <span><i className="st-open">○</i> open</span>
        <span><i className="cl-waiting">◆</i><i className="cl-idle">◆</i> claude wants you</span>
        <span><i className="cl-working">◌</i> busy</span>
      </div>
    </aside>
  )
}

function RemovalMark({ r }: { r: rm.Removal }) {
  if (r.kind === 'removing' || r.kind === 'deleting') return <span className="spinner" aria-hidden="true" />
  return <i className="st-active" aria-hidden="true">✓</i>
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
