import { Fragment, useId, useRef, useState } from 'react'
import type { main } from '../wailsjs/go/models'
import { ClipboardSetText } from '../wailsjs/runtime/runtime'
import { Menu, type MenuItem } from './Menu'
import * as rm from './removal'
import { removeText, type Group } from './repos'
import { key, startDrag } from './util'

export type RemovalAction = 'force' | 'keep' | 'delete-branch' | 'dismiss'

const MIN_WIDTH = 180
const MAX_WIDTH = 480

// sidebarWidth clamps a dragged or nudged sidebar width, also to half the window.
export const sidebarWidth = (w: number) => Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, innerWidth / 2, w)))

const mac = navigator.userAgent.includes('Mac')

// Sidebar is the worktree list: a filter box (⌘P) over one row per worktree,
// grouped by repo under headers with a ⋯ menu. Worktrees are keyed by dir: two
// repos in one window can share worktree names.
export function Sidebar(props: {
  snap: main.Snapshot | null
  all: main.WorkspaceInfo[]
  groups: Group[]
  filtered: main.WorkspaceInfo[] // the rows shown, in order: what the cursor moves over
  selected: string | null
  cursor: number
  listFocused: boolean
  filter: string
  filterRef: React.RefObject<HTMLInputElement | null>
  onFilterChange: (v: string) => void
  onFilterFocus: () => void
  onFilterBlur: () => void
  onFilterKey: (e: React.KeyboardEvent) => void
  onOpen: (dir: string) => void
  removals: rm.Removals
  onRemoval: (r: rm.Removal, action: RemovalAction) => void
  onHold: (dir: string, held: boolean) => void
  collapsed: string[] // repo paths
  onNewIn: (repoPath: string) => void
  onToggle: (repoPath: string) => void
  onReveal: (repoPath: string) => void
  onAskRemove: (repoPath: string) => void
  confirm: string | null // the repo whose Remove from window is being confirmed
  onConfirm: (remove: boolean) => void
  width: number
  onResize: (width: number) => void
}) {
  const { snap, all, filtered, selected, cursor, listFocused, filter, filterRef, removals } = props
  const listRef = useRef<HTMLUListElement>(null)
  // repo: the header whose ⋯ opened it, for aria-expanded.
  const [menu, setMenu] = useState<{ label: string; at: { x: number; y: number }; items: MenuItem[]; repo?: string } | null>(null)
  const [dragging, setDragging] = useState(false)

  const rowMenu = (w: main.WorkspaceInfo, at: { x: number; y: number }) =>
    setMenu({
      label: `Actions for ${w.name}`,
      at,
      items: [
        ...(w.branch ? [{ label: 'Copy branch', onSelect: () => ClipboardSetText(w.branch) }] : []),
        { label: 'Copy path', onSelect: () => ClipboardSetText(w.dir) },
      ],
    })
  const repoMenu = (repo: main.RepoInfo, button: HTMLElement) => {
    const b = button.getBoundingClientRect()
    setMenu({
      label: `Actions for ${repo.name}`,
      at: { x: b.left, y: b.bottom + 2 },
      repo: repo.path,
      items: [
        { label: 'New worktree…', onSelect: () => props.onNewIn(repo.path) },
        { label: props.collapsed.includes(repo.path) ? 'Expand' : 'Collapse', onSelect: () => props.onToggle(repo.path) },
        { label: mac ? 'Reveal in Finder' : 'Open Folder', onSelect: () => props.onReveal(repo.path) },
        // The window's own repo can't go: the window is tied to it.
        ...(repo.added ? ['divider' as const, { label: 'Remove from window…', danger: true, onSelect: () => props.onAskRemove(repo.path) }] : []),
      ],
    })
  }

  // Shift+F10 or the context-menu key opens the cursor row's menu from the filter.
  const onFilterKey = (e: React.KeyboardEvent) => {
    const w = filtered[cursor]
    const li = listRef.current?.querySelector('li.cursor')
    if ((e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) && w && li) {
      e.preventDefault()
      const r = li.getBoundingClientRect()
      rowMenu(w, { x: r.left + 24, y: r.bottom })
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
        {props.groups.map((g) => {
          const path = g.repo?.path ?? ''
          if (g.repo && path === props.confirm) {
            const text = removeText(g.repo.name, all.filter((w) => w.repoPath === path))
            return <RepoConfirm key={'repo:' + path} {...text} onAnswer={props.onConfirm} />
          }
          const hidden = g.repo && props.collapsed.includes(path) && !g.rows.length ? all.filter((w) => w.repoPath === path).length : 0
          return (
            <Fragment key={'repo:' + path}>
              <li className="group" onMouseDown={(e) => e.preventDefault()}>
                <span className="group-name">{g.repo ? g.repo.name : 'Other'}</span>
                {g.repo?.added && <span className="tag">added</span>}
                {hidden > 0 && <span className="tag">{hidden} hidden</span>}
                {g.repo && (
                  <button
                    className="group-menu"
                    data-repo={path}
                    aria-label={`Actions for ${g.repo.name}`}
                    aria-haspopup="menu"
                    aria-expanded={menu?.repo === path}
                    title={`Actions for ${g.repo.name}`}
                    onClick={(e) => repoMenu(g.repo!, e.currentTarget)}
                  >
                    ⋯
                  </button>
                )}
              </li>
              {g.rows.map((w) => {
                const r = removals[w.dir]
                const n = all.indexOf(w)
                // A kept branch's row waits while it is hovered or focused.
                const hold = r?.kind === 'kept' ? {
                  onMouseOver: () => props.onHold(w.dir, true),
                  onMouseLeave: (e: React.MouseEvent<HTMLElement>) => props.onHold(w.dir, e.currentTarget.contains(document.activeElement)),
                  onFocus: () => props.onHold(w.dir, true),
                  onBlur: (e: React.FocusEvent<HTMLElement>) =>
                    props.onHold(w.dir, e.currentTarget.matches(':hover') || e.currentTarget.contains(e.relatedTarget as Node | null)),
                } : {}
                const cls = [
                  w.dir === selected && 'sel',
                  listFocused && filtered[cursor]?.dir === w.dir && 'cursor',
                  r && 'rm-' + r.kind,
                  r?.kind === 'done' && r.collapsing && 'collapsing',
                ]
                return (
                  <li
                    key={w.dir}
                    className={cls.filter(Boolean).join(' ')}
                    aria-busy={rm.inFlight(r) || undefined}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => rm.usable(r) && props.onOpen(w.dir)}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      rowMenu(w, { x: e.clientX, y: e.clientY })
                    }}
                    title={w.dir}
                    {...hold}
                  >
                    <span className="gutter">
                      {r && !rm.usable(r) ? (
                        <RemovalMark r={r} />
                      ) : (
                        <>
                          <Status w={w} active={w.dir === selected} />
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
                          <span className="rm-line" title={r.detail || undefined}>
                            {r.reason === 'unmerged' ? 'Removed. Branch kept (unmerged).' : `Removed. Branch kept: ${r.reason}`}
                          </span>
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
            </Fragment>
          )
        })}
        {props.groups.length === 0 && <li className="empty">{all.length ? 'no match' : 'no worktrees'}</li>}
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
      {menu && <Menu label={menu.label} at={menu.at} onClose={() => setMenu(null)} items={menu.items} />}
    </aside>
  )
}

// RepoConfirm stands in for a repo's group while taking it out of the window is
// confirmed. It takes focus (Cancel, the safe answer); Escape cancels.
function RepoConfirm({ title, detail, onAnswer }: { title: string; detail: string; onAnswer: (remove: boolean) => void }) {
  const id = useId()
  return (
    <li
      className="repo-confirm"
      role="group"
      aria-labelledby={id + 'q'}
      aria-describedby={id + 'd'}
      onKeyDown={(e) => {
        if (e.key !== 'Escape') return
        e.preventDefault()
        e.stopPropagation()
        onAnswer(false)
      }}
    >
      <span className="rm-q" id={id + 'q'}>
        {title}
      </span>
      <span className="rm-line" id={id + 'd'}>
        {detail}
      </span>
      <span className="rm-actions">
        <button autoFocus onClick={() => onAnswer(false)}>
          Cancel
        </button>
        <button className="danger" onClick={() => onAnswer(true)}>
          Remove
        </button>
      </span>
    </li>
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
