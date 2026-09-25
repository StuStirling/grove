import type { main } from '../wailsjs/go/models'
import { key } from './util'

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
}) {
  const { snap, all, filtered, selected, cursor, listFocused, filter, filterRef } = props
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
        {filtered.map((w, i) => (
          <li
            key={w.name}
            className={(w.name === selected ? 'sel ' : '') + (listFocused && i === cursor ? 'cursor' : '')}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => props.onOpen(w.name)}
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
  )
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
