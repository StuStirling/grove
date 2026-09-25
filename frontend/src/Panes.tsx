import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import '@xterm/xterm/css/xterm.css'
import { OpenURL, Size, Write } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'
import type { main } from '../wailsjs/go/models'
import { activate, type Layout } from './layout'
import { Menu } from './Menu'
import { key } from './util'

// Live xterm instances by pane id, so the app can move focus between panes.
export const terms = new Map<string, Terminal>()

const theme = {
  background: '#0e1116', foreground: '#d7dde5', cursor: '#7ee787', cursorAccent: '#0e1116',
  selectionBackground: 'rgba(121, 192, 255, 0.3)',
  black: '#1f2630', red: '#ff7b72', green: '#7ee787', yellow: '#e3b341', blue: '#79c0ff',
  magenta: '#d2a8ff', cyan: '#56d4dd', white: '#d7dde5',
  brightBlack: '#6e7681', brightRed: '#ffa198', brightGreen: '#aff5b4', brightYellow: '#f8e3a1',
  brightBlue: '#a5d6ff', brightMagenta: '#e2c5ff', brightCyan: '#b3f0ff', brightWhite: '#ffffff',
}

function decode(b64: string): Uint8Array {
  const s = atob(b64)
  const out = new Uint8Array(s.length)
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i)
  return out
}

type Font = { family: string; size: number }

// TermPane is one xterm bound to a backend pane. Its first Size call starts the
// pane's process at the drawn size.
function TermPane({ pane, font, onFocus }: { pane: main.Pane; font: Font; onFocus: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const fitRef = useRef<() => void>(() => {})
  const focusRef = useRef(onFocus)
  focusRef.current = onFocus

  useEffect(() => {
    const el = ref.current!
    const term = new Terminal({
      fontFamily: font.family, fontSize: font.size, theme,
      cursorBlink: true, scrollback: 10000, allowProposedApi: true,
      macOptionClickForcesSelection: true, // ⌥-drag selects even when an app grabs the mouse
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.loadAddon(new WebLinksAddon((e, uri) => (e.metaKey || e.ctrlKey) && OpenURL(uri)))
    term.loadAddon(new Unicode11Addon())
    term.unicode.activeVersion = '11' // emoji/symbol widths as Claude Code lays them out
    term.open(el)

    // Shift+Enter inserts a newline in Claude Code (and zsh) as ESC CR; xterm.js
    // would otherwise send a bare CR and submit.
    term.attachCustomKeyEventHandler((e) => {
      if (e.key === 'Enter' && e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey) {
        if (e.type === 'keydown') Write(pane.id, '\x1b\r')
        return false
      }
      return true
    })
    // Subscribe before the first Size so no output is missed.
    const off = EventsOn('pty:' + pane.id, (b64: string) => term.write(decode(b64)))
    const data = term.onData((d) => Write(pane.id, d))
    const focus = () => focusRef.current()
    term.textarea?.addEventListener('focus', focus)

    let last = ''
    fitRef.current = () => {
      if (el.clientWidth === 0 || el.clientHeight === 0) return
      fit.fit()
      const key = `${term.cols}x${term.rows}`
      if (key !== last) {
        last = key
        Size(pane.id, term.cols, term.rows).catch(() => {})
      }
    }
    const ro = new ResizeObserver(() => fitRef.current())
    ro.observe(el)
    fitRef.current()
    terms.set(pane.id, term)

    return () => {
      terms.delete(pane.id)
      ro.disconnect()
      off()
      data.dispose()
      term.textarea?.removeEventListener('focus', focus)
      term.dispose()
    }
  }, [pane.id])

  useEffect(() => {
    const term = terms.get(pane.id)
    if (!term) return
    term.options.fontFamily = font.family
    term.options.fontSize = font.size
    fitRef.current()
  }, [font.family, font.size, pane.id])

  return <div className="term" ref={ref} />
}

const markNote: Record<string, string> = { waiting: 'Claude is waiting for permission', idle: 'Claude finished: your turn' }

// WorkspaceView is one worktree: a tab bar over a body per pane. Every terminal
// is a direct child of the one grid, keyed by pane id and in backend order, and
// only its grid cell and visibility change, so showing, moving or splitting
// tabs never remounts one (that would lose its scrollback). Hidden tabs stay
// laid out in their cell, so they are already fitted when shown.
export function WorkspaceView(props: {
  ws: main.WorkspaceInfo
  layout: Layout
  visible: boolean
  font: Font
  snap: main.Snapshot | null
  onLayout: (f: (l: Layout) => Layout, focus?: boolean) => void // focus: then focus the focused tab's terminal
  onNewTab: (kind: 'claude' | 'shell', pane: number) => void
  onCloseTab: (id: string) => void
}) {
  const { ws, layout: l, visible, font, snap, onLayout } = props
  const [menu, setMenu] = useState<{ pane: number; at: { x: number; y: number } } | null>(null)
  const byId = new Map((ws.panes ?? []).map((p) => [p.id, p]))
  const paneOf = (id: string) => l.panes.findIndex((p) => p.tabs.includes(id))

  return (
    <div className={'workspace' + (visible ? '' : ' hidden')}>
      {l.panes.map((p, i) => (
        <div key={i} className="tabbar" style={{ gridColumn: 1 }}>
          <div className="tabs" role="tablist" aria-label="Tabs">
            {p.tabs.map((id) => {
              const t = byId.get(id)
              if (!t) return null
              const label = l.labels[id] ?? t.name
              const current = i === l.focus && id === p.active
              const note = current ? '' : (markNote[t.claude] ?? '')
              return (
                <div key={id} className={'tab' + (id === p.active ? ' active' : '') + (current ? ' current' : '')}>
                  <button role="tab" aria-selected={id === p.active} aria-label={note ? `${label}, ${note}` : label} title={note || undefined} onClick={() => onLayout((l) => activate(l, id))}>
                    <i className={'tab-icon ' + t.kind} aria-hidden>
                      {t.kind === 'claude' ? '✻' : '>_'}
                    </i>
                    {label}
                    {note && (
                      <i className={'cl-' + t.claude} aria-hidden>
                        ◆
                      </i>
                    )}
                  </button>
                  <button className="tab-x" aria-label={`Close tab ${label}`} title={`Close tab ${label}`} onClick={() => props.onCloseTab(id)}>
                    ×
                  </button>
                </div>
              )
            })}
          </div>
          <button
            className="tool"
            aria-label="New tab"
            aria-haspopup="menu"
            title="New tab"
            onClick={(e) => {
              const r = e.currentTarget.getBoundingClientRect()
              setMenu({ pane: i, at: { x: r.left, y: r.bottom + 2 } })
            }}
          >
            +
          </button>
        </div>
      ))}
      {(ws.panes ?? []).map((t) => {
        const i = paneOf(t.id)
        if (i < 0) return null
        const shown = l.panes[i].active === t.id
        return (
          <div key={t.id} className="pane" style={{ gridColumn: 1, visibility: shown ? undefined : 'hidden' }} onMouseDown={() => onLayout((l) => activate(l, t.id))}>
            <TermPane pane={t} font={font} onFocus={() => onLayout((l) => activate(l, t.id), false)} />
          </div>
        )
      })}
      {menu && visible && (
        <Menu
          label="New tab"
          at={menu.at}
          onClose={() => setMenu(null)}
          items={[
            { label: 'New Claude tab', keys: key(snap, 'New Claude Tab'), onSelect: () => props.onNewTab('claude', menu.pane) },
            { label: 'New shell', keys: key(snap, 'New Shell'), onSelect: () => props.onNewTab('shell', menu.pane) },
          ]}
        />
      )}
    </div>
  )
}
