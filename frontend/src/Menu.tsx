import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export type MenuItem = { label: string; keys?: string; danger?: boolean; onSelect: () => void } | 'divider'

// Menu is a small popup menu at a viewport point (a right-click, or under its
// button). ↑↓ move, ↩ picks, esc / ⇥ / a click elsewhere closes; focus goes back
// to wherever it was when the menu opened.
export function Menu({ items, at, label, onClose }: { items: MenuItem[]; at: { x: number; y: number }; label: string; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState(at)

  useLayoutEffect(() => {
    const el = ref.current!
    const r = el.getBoundingClientRect()
    setPos({ x: Math.max(4, Math.min(at.x, innerWidth - r.width - 4)), y: Math.max(4, Math.min(at.y, innerHeight - r.height - 4)) })
    const prev = document.activeElement as HTMLElement | null
    el.querySelector<HTMLElement>('[role=menuitem]')?.focus()
    return () => {
      if (!document.activeElement || document.activeElement === document.body || el.contains(document.activeElement)) prev?.focus()
    }
  }, [at.x, at.y])

  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const down = (e: PointerEvent) => !ref.current?.contains(e.target as Node) && close.current()
    const blur = () => close.current()
    window.addEventListener('pointerdown', down, true)
    window.addEventListener('blur', blur)
    return () => {
      window.removeEventListener('pointerdown', down, true)
      window.removeEventListener('blur', blur)
    }
  }, [])

  const onKey = (e: React.KeyboardEvent) => {
    const els = [...ref.current!.querySelectorAll<HTMLElement>('[role=menuitem]')]
    const i = els.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      els[(i + (e.key === 'ArrowDown' ? 1 : -1) + els.length) % els.length]?.focus()
    } else if (e.key === 'Escape' || e.key === 'Tab') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }

  return (
    <div className="menu" role="menu" aria-label={label} ref={ref} style={{ left: pos.x, top: pos.y }} onKeyDown={onKey}>
      {items.map((it, i) =>
        it === 'divider' ? (
          <div key={i} className="menu-divider" role="separator" />
        ) : (
          <button
            key={i}
            role="menuitem"
            className={it.danger ? 'danger' : undefined}
            onClick={() => {
              onClose()
              it.onSelect()
            }}
          >
            <span>{it.label}</span>
            {it.keys && <kbd>{it.keys}</kbd>}
          </button>
        ),
      )}
    </div>
  )
}
