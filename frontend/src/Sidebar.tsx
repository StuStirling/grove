import { useRef, useState } from 'react'
import type { main } from '../wailsjs/go/models'
import { ClipboardSetText } from '../wailsjs/runtime/runtime'
import { Menu } from './Menu'
import * as rm from './removal'
import { key, startDrag } from './util'

export type RemovalAction = 'force' | 'keep' | 'delete-branch' | 'dismiss'

const MIN_WIDTH = 180
const MAX_WIDTH = 480

// sidebarWidth clamps a dragged or nudged sidebar width, also to half the window.
export const sidebarWidth = (w: number) => Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, innerWidth / 2, w)))

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
  width: number
  onResize: (width: number) => void
}) {
  const { snap, all, filtered, selected, cursor, listFocused, filter, filterRef, removals } = props
  const listRef = useRef<HTMLUListElement>(null)
  const [menu, setMenu] = useState<{ ws: main.WorkspaceInfo; at: { x: number; y: number } } | null>(null)
  const [dragging, setDragging] = useState(false)

  // Shift+F10 or the context-menu key opens the cursor row's menu from the filter.
  const onFilterKey = (e: React.KeyboardEvent) => {
    const w = filtered[cursor]
    const li = listRef.current?.querySelector('li.cursor')
    if ((e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) && w && li) {
      e.preventDefault()
      const r = li.getBoundingClientRect()
      setMenu({ ws: w, at: { x: r.left + 24, y: r.bottom } })
    } else props.onFilterKey(e)
  }
  // Focus going to the row menu and back doesn't leave the list, so the cursor stays.
  const viaMenu = (e: React.FocusEvent) => !!(e.relatedTarget as Element | null)?.closest('.menu')

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
        onFocus={(e) => viaMenu(e) || props.onFilterFocus()}
        onBlur={(e) => viaMenu(e) || props.onFilterBlur()}
        onKeyDown={onFilterKey}
      />
      <ul className="list" ref={listRef}>
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
              onContextMenu={(e) => {
                e.preventDefault()
                setMenu({ ws: w, at: { x: e.clientX, y: e.clientY } })
              }}
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
      <div
        className={'sidebar-resize' + (dragging ? ' dragging' : '')}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize sidebar"
        aria-valuenow={props.width}
        aria-valuemin={MIN_WIDTH}
        aria-valuemax={MAX_WIDTH}
        tabIndex={0}
        onPointerDown={(e) => {
          setDragging(true)
          startDrag(e, (ev) => props.onResize(ev.clientX), () => setDragging(false)) // the sidebar starts at x = 0
        }}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return
          e.preventDefault()
          props.onResize(props.width + (e.key === 'ArrowRight' ? 10 : -10))
        }}
      />
      {menu && (
        <Menu
          label={`Actions for ${menu.ws.name}`}
          at={menu.at}
          onClose={() => setMenu(null)}
          items={[
            ...(menu.ws.branch ? [{ label: 'Copy branch', onSelect: () => ClipboardSetText(menu.ws.branch) }] : []),
            { label: 'Copy path', onSelect: () => ClipboardSetText(menu.ws.dir) },
          ]}
        />
      )}
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
