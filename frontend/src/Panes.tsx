import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import '@xterm/xterm/css/xterm.css'
import { OpenURL, Size, Write } from '../wailsjs/go/main/App'
import { EventsOn } from '../wailsjs/runtime/runtime'
import type { main } from '../wailsjs/go/models'
import { startDrag } from './util'

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

// Layout: the first pane is the big left column; the rest stack in a right
// column. Splitters are draggable.
export function WorkspaceView(props: {
  ws: main.WorkspaceInfo
  visible: boolean
  focused: number
  zoomed: number | null
  font: Font
  onFocus: (i: number) => void
}) {
  const { ws, visible, focused, zoomed, font, onFocus } = props
  const panes = ws.panes ?? []
  const [left, setLeft] = useState(60) // % width of the big pane
  const [weights, setWeights] = useState<number[]>([])
  const rootRef = useRef<HTMLDivElement>(null)
  const colRef = useRef<HTMLDivElement>(null)

  const right = panes.slice(1)
  const w = weights.length === right.length ? weights : right.map(() => 1 / right.length)

  const dragLeft = (e: React.PointerEvent) =>
    startDrag(e, (ev) => {
      const r = rootRef.current!.getBoundingClientRect()
      setLeft(Math.min(85, Math.max(15, ((ev.clientX - r.left) / r.width) * 100)))
    })

  // Divider i sits between right panes i and i+1.
  const dragRight = (i: number) => (e: React.PointerEvent) =>
    startDrag(e, (ev) => {
      const r = colRef.current!.getBoundingClientRect()
      const y = (ev.clientY - r.top) / r.height
      const start = w.slice(0, i).reduce((a, b) => a + b, 0)
      const span = w[i] + w[i + 1]
      const min = Math.min(0.08, span / 2)
      const a = Math.min(span - min, Math.max(min, y - start))
      const next = [...w]
      next[i] = a
      next[i + 1] = span - a
      setWeights(next)
    })

  const cell = (p: main.Pane, i: number) => (
    <div
      key={p.id}
      className={'pane' + (i === focused ? ' focused' : '') + (zoomed === i ? ' zoomed' : '')}
      style={i > 0 ? { flex: w[i - 1] } : undefined}
      onMouseDown={() => onFocus(i)}
    >
      <TermPane pane={p} font={font} onFocus={() => onFocus(i)} />
    </div>
  )

  return (
    <div className={'workspace' + (visible ? '' : ' hidden') + (zoomed !== null ? ' has-zoom' : '')} ref={rootRef}>
      {panes.length > 0 && (
        <div className="col" style={{ width: right.length ? `${left}%` : '100%' }}>
          {cell(panes[0], 0)}
        </div>
      )}
      {right.length > 0 && (
        <>
          <div className="split split-v" onPointerDown={dragLeft} />
          <div className="col col-right" ref={colRef}>
            {right.map((p, j) => (
              <PaneWithDivider key={p.id} last={j === right.length - 1} onDrag={dragRight(j)}>
                {cell(p, j + 1)}
              </PaneWithDivider>
            ))}
          </div>
        </>
      )}
    </div>
  )
}

function PaneWithDivider(props: { children: React.ReactNode; last: boolean; onDrag: (e: React.PointerEvent) => void }) {
  return (
    <>
      {props.children}
      {!props.last && <div className="split split-h" onPointerDown={props.onDrag} />}
    </>
  )
}
