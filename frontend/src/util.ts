import type { main } from '../wailsjs/go/models'

export const errText = (e: unknown) => String((e as Error)?.message ?? e)

// key returns a shortcut's label (e.g. "⌘P") from the backend's table.
export function key(snap: main.Snapshot | null, label: string) {
  return snap?.shortcuts?.find((s) => s.label === label)?.keys ?? ''
}

// startDrag follows a pointer drag on a divider until release, calling move with
// each pointer position and done once at the end (e.g. to save the new size).
export function startDrag(e: React.PointerEvent, move: (ev: PointerEvent) => void, done?: () => void) {
  e.preventDefault()
  const target = e.currentTarget as HTMLElement
  target.setPointerCapture(e.pointerId)
  document.body.classList.add('dragging')
  const up = () => {
    target.removeEventListener('pointermove', move)
    target.removeEventListener('pointerup', up)
    target.removeEventListener('pointercancel', up)
    document.body.classList.remove('dragging')
    done?.()
  }
  target.addEventListener('pointermove', move)
  target.addEventListener('pointerup', up)
  target.addEventListener('pointercancel', up)
}

// keepFocus returns focus to a dialog when something outside it takes focus
// (a pane mounting, a placeholder button), so its keys keep working.
export const keepFocus = (e: React.FocusEvent<HTMLElement>) => {
  const el = e.currentTarget
  if (el.contains(e.relatedTarget as Node | null)) return
  requestAnimationFrame(() => {
    if (el.isConnected && !el.contains(document.activeElement)) (el.querySelector('input') ?? el).focus()
  })
}
