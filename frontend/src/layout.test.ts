import { test } from 'node:test'
import assert from 'node:assert/strict'
import { activate, closeTab, cycle, focusedTab, place, sync, type Layout, type Tab } from './layout.ts'

const claude = (id: string): Tab => ({ id, name: 'claude' })
const zsh = (id: string): Tab => ({ id, name: 'zsh' })
const tabs = (l: Layout) => l.panes.map((p) => p.tabs)
const actives = (l: Layout) => l.panes.map((p) => p.active)

test('a worktree opens with its tabs in one pane', () => {
  const l = sync(undefined, [claude('a'), zsh('b'), { id: 'c', name: 'lazygit' }])
  assert.deepEqual(tabs(l), [['a', 'b', 'c']])
  assert.equal(focusedTab(l), 'a')
  assert.deepEqual(l.labels, { a: 'claude', b: 'zsh', c: 'lazygit' })
  assert.deepEqual(tabs(sync(undefined, [])), [[]])
})

test('sync closes tabs that are gone and adds new ones to the focused pane', () => {
  let l = sync(undefined, [claude('a'), zsh('b'), zsh('c')])
  l = activate(l, 'b')
  l = sync(l, [claude('a'), zsh('c'), zsh('d')])
  assert.deepEqual(tabs(l), [['a', 'c', 'd']])
  assert.equal(focusedTab(l), 'c') // the closed tab's right neighbour, not the new tab
  assert.deepEqual(l.labels, { a: 'claude', c: 'zsh 2', d: 'zsh' })
})

test('place is idempotent, whichever of sync and place sees a new tab first', () => {
  const base = sync(undefined, [claude('a'), zsh('b')])
  const syncFirst = place(sync(base, [claude('a'), zsh('b'), zsh('n')]), zsh('n'), 0)
  const placeFirst = sync(place(base, zsh('n'), 0), [claude('a'), zsh('b'), zsh('n')])
  assert.deepEqual(syncFirst, placeFirst)
  assert.deepEqual(place(syncFirst, zsh('n'), 0), syncFirst)
  assert.deepEqual(tabs(syncFirst), [['a', 'b', 'n']])
  assert.equal(focusedTab(syncFirst), 'n')
  assert.equal(syncFirst.labels.n, 'zsh 2')
})

test('place at an index reorders', () => {
  const l = sync(undefined, [claude('a'), zsh('b'), zsh('c')])
  assert.deepEqual(tabs(place(l, zsh('c'), 0, 0)), [['c', 'a', 'b']])
  assert.deepEqual(tabs(place(l, claude('a'), 0, 2)), [['b', 'c', 'a']])
})

test('closing a tab shows its right neighbour, else its left', () => {
  let l = activate(sync(undefined, [claude('a'), zsh('b'), zsh('c')]), 'b')
  l = closeTab(l, 'b')
  assert.equal(focusedTab(l), 'c')
  l = closeTab(l, 'c')
  assert.equal(focusedTab(l), 'a')
  assert.equal(focusedTab(closeTab(activate(l, 'a'), 'x')), 'a') // unknown tab: no change
  // Closing a background tab keeps the shown one; the last tab leaves an empty pane.
  l = closeTab(l, 'a')
  assert.deepEqual(tabs(l), [[]])
  assert.equal(focusedTab(l), null)
})

test('labels take the lowest free number and never renumber', () => {
  let l = sync(undefined, [zsh('a'), zsh('b'), zsh('c')])
  assert.deepEqual(l.labels, { a: 'zsh', b: 'zsh 2', c: 'zsh 3' })
  l = sync(l, [zsh('b'), zsh('c')])
  l = sync(l, [zsh('b'), zsh('c'), zsh('d'), zsh('e')])
  assert.deepEqual(l.labels, { b: 'zsh 2', c: 'zsh 3', d: 'zsh', e: 'zsh 4' })
})

test('cycle wraps around the focused pane', () => {
  const l = sync(undefined, [claude('a'), zsh('b'), zsh('c')])
  assert.equal(focusedTab(cycle(l, -1)), 'c')
  assert.equal(focusedTab(cycle(cycle(l, 1), 1)), 'c')
  assert.equal(focusedTab(cycle(activate(l, 'c'), 1)), 'a')
})
