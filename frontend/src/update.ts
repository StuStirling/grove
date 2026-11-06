import type { main } from '../wailsjs/go/models'

// Copy and keys for the Update from Base dialog, kept apart from the component
// so they can be tested.

const commits = (n: number) => `${n} ${n === 1 ? 'commit' : 'commits'}`

export function prLine(pr: main.PRInfo): string {
  switch (pr.state) {
    case 'reviewed':
      return `PR #${pr.number} · reviewed by ${pr.reviewer}`
    case 'unreviewed':
      return `PR #${pr.number} · not reviewed`
    case 'merged':
      return `PR #${pr.number} · merged`
    case 'unknown':
      return `PR state unknown (${pr.detail})`
  }
  return 'No PR yet'
}

export function stepText(s: main.UpdateStep, strategy: string): string {
  switch (s.kind) {
    case 'pull':
      return `Pull ${commits(s.count)} from ${s.ref}`
    case 'ff':
      return `Fast-forward from ${s.ref} (${commits(s.count)})`
  }
  const behind = `(${commits(s.count)} behind)`
  if (strategy === 'rebase') return `Rebase onto ${s.ref} ${behind}`
  if (strategy === 'merge') return `Merge ${s.ref} in ${behind}`
  return `Update from ${s.ref} ${behind}`
}

export function pushLabel(kind: string): string {
  return kind === 'force' ? 'Force push' : kind === 'upstream' ? 'Push and set upstream' : 'Push'
}

export function pushQuestion(kind: string, pr: main.PRInfo, remote: string): string {
  const open = pr.state === 'unreviewed' || pr.state === 'reviewed'
  switch (kind) {
    case 'force':
      return `Your branch has diverged from ${remote}. Force push (with lease)${open ? ` to update PR #${pr.number}` : ''}?`
    case 'upstream':
      return `This branch isn't on ${remote} yet. Push it and set upstream?`
  }
  return open ? `Push to update PR #${pr.number}?` : `Push to ${remote}?`
}

export function doneLine(strategy: string, steps: main.UpdateStep[]): string {
  const base = steps.find((s) => s.kind === 'base')
  if (base) return strategy === 'merge' ? `Merged ${base.ref} in` : `Rebased onto ${base.ref}`
  const s = steps[0]
  return s?.kind === 'ff' ? `Fast-forwarded from ${s.ref}` : `Pulled ${s?.ref ?? ''}`
}

export type View = 'plan' | 'push' | 'conflicts'
export type Action = 'update' | 'rebase' | 'merge' | 'cancel' | 'push' | 'continue' | 'claude' | 'abort'

// keyAction is what a key does in each of the dialog's views. Letters only
// count unmodified, so ⌘R stays Reload. Abort takes the platform's delete
// modifier because it throws away conflict resolution.
export function keyAction(view: View, e: { key: string; metaKey: boolean; ctrlKey: boolean }, mac: boolean): Action | null {
  if (e.key === 'Escape') return 'cancel'
  const plain = !e.metaKey && !e.ctrlKey
  const k = e.key.toLowerCase()
  switch (view) {
    case 'plan':
      if (e.key === 'Enter') return 'update'
      if (plain && k === 'r') return 'rebase'
      if (plain && k === 'm') return 'merge'
      return null
    case 'push':
      return e.key === 'Enter' ? 'push' : null
    case 'conflicts':
      if (e.key === 'Enter') return 'continue'
      if (plain && k === 'c') return 'claude'
      if (e.key === 'Backspace' && (mac ? e.metaKey : e.ctrlKey)) return 'abort'
      return null
  }
}
