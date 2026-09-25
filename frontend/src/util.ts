import type { main } from '../wailsjs/go/models'

export const errText = (e: unknown) => String((e as Error)?.message ?? e)

// key returns a shortcut's label (e.g. "⌘P") from the backend's table.
export function key(snap: main.Snapshot | null, label: string) {
  return snap?.shortcuts?.find((s) => s.label === label)?.keys ?? ''
}
