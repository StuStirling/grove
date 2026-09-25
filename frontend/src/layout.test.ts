import { test } from 'node:test'
import assert from 'node:assert/strict'
import { activate, closePane, closeTab, cycle, focusPane, focusedTab, moveToOther, place, split, sync, toggleZoom, type Layout, type Tab } from './layout.ts'

const claude = (id: string): Tab => ({ id, name: 'claude' })
const zsh = (id: string): Tab => ({ id, name: 'zsh' })
const tabs = (l: Layout) => l.panes.map((p) => p.tabs)
// one is a worktree that opened with its first tab, then gained the others, so
// they all sit in one pane.
const one = (...ts: Tab[]) => sync(sync(undefined, ts.slice(0, 1), 0.5), ts, 0.5)

test('a worktree opens with its first tab on the left and the rest in a right pane', () => {
  const l = sync(undefined, [claude('a'), zsh('b'), { id: 'c', name: 'lazygit' }], 0.3)
  assert.deepEqual(tabs(l), [['a'], ['b', 'c']])
  assert.deepEqual(l.panes.map((p) => p.active), ['a', 'b'])
  assert.equal(focusedTab(l), 'a')
  assert.equal(l.ratio, 0.3)
  assert.deepEqual(l.labels, { a: 'claude', b: 'zsh', c: 'lazygit' })
  assert.deepEqual(tabs(sync(undefined, [claude('a')], 0.5)), [['a']]) // one configured pane: unsplit
  assert.deepEqual(tabs(sync(undefined, [], 0.5)), [[]])
})

test('sync closes tabs that are gone and adds new ones to the focused pane', () => {
  let l = activate(sync(undefined, [claude('a'), zsh('b'), zsh('c')], 0.5), 'b')
  l = sync(l, [claude('a'), zsh('c'), zsh('d')], 0.5)
  assert.deepEqual(tabs(l), [['a'], ['c', 'd']])
  assert.equal(focusedTab(l), 'c') // the closed tab's right neighbour, not the new tab
  assert.deepEqual(l.labels, { a: 'claude', c: 'zsh 2', d: 'zsh' })
})

test('place is idempotent, whichever of sync and place sees a new tab first', () => {
  const cases: [Layout, Tab[], number][] = [
    [sync(undefined, [claude('a'), zsh('b')], 0.5), [claude('a'), zsh('b'), zsh('n')], 0], // a new tab in the focused pane
    [sync(undefined, [claude('a')], 0.5), [claude('a'), zsh('n')], 1], // a shell to split with
  ]
  for (const [base, live, i] of cases) {
    const syncFirst = place(sync(base, live, 0.5), zsh('n'), i)
    const placeFirst = sync(place(base, zsh('n'), i), live, 0.5)
    assert.deepEqual(syncFirst, placeFirst)
    assert.deepEqual(place(syncFirst, zsh('n'), i), syncFirst)
    assert.equal(focusedTab(syncFirst), 'n')
    assert.equal(syncFirst.focus, i)
  }
})

test('place at an index reorders', () => {
  const l = one(claude('a'), zsh('b'), zsh('c'))
  assert.deepEqual(tabs(place(l, zsh('c'), 0, 0)), [['c', 'a', 'b']])
  assert.deepEqual(tabs(place(l, claude('a'), 0, 2)), [['b', 'c', 'a']])
})

test('closing a tab shows its right neighbour, else its left', () => {
  let l = activate(one(claude('a'), zsh('b'), zsh('c')), 'b')
  l = closeTab(l, 'b')
  assert.equal(focusedTab(l), 'c')
  l = closeTab(l, 'c')
  assert.equal(focusedTab(l), 'a')
  assert.equal(focusedTab(closeTab(l, 'x')), 'a') // unknown tab: no change
  // The last tab leaves one empty pane.
  l = closeTab(l, 'a')
  assert.deepEqual(tabs(l), [[]])
  assert.equal(focusedTab(l), null)
})

test('closing the last tab of a pane unsplits and ends zoom', () => {
  const l = toggleZoom(focusPane(sync(undefined, [claude('a'), zsh('b')], 0.5), 1))
  assert.equal(l.zoom, true)
  const u = closeTab(l, 'b')
  assert.deepEqual(tabs(u), [['a']])
  assert.equal(u.focus, 0)
  assert.equal(u.zoom, false)
  assert.equal(toggleZoom(u).zoom, false) // nothing to zoom while unsplit
})

test('split moves the focused tab to a new right pane', () => {
  const l = split(activate(one(claude('a'), zsh('b'), zsh('c')), 'b'))!
  assert.deepEqual(tabs(l), [['a', 'c'], ['b']])
  assert.equal(l.focus, 1)
  assert.equal(focusedTab(l), 'b')
  assert.equal(l.panes[0].active, 'c')
  assert.equal(split(l), l) // already split
})

test('split with a single tab asks for a shell, placed in the right pane', () => {
  const l = one(claude('a'))
  assert.equal(split(l), null)
  const s = place(l, zsh('s'), 1)
  assert.deepEqual(tabs(s), [['a'], ['s']])
  assert.equal(focusedTab(s), 's')
})

test('moving a tab to the other pane unsplits when its pane empties', () => {
  let l = sync(undefined, [claude('a'), zsh('b'), zsh('c')], 0.5)
  l = moveToOther(l, 1)
  assert.deepEqual(tabs(l), [['a', 'b'], ['c']])
  assert.equal(focusedTab(l), 'b')
  l = moveToOther(l, 1)
  assert.deepEqual(tabs(l), [['a', 'b', 'c']])
  assert.equal(focusedTab(l), 'c')
  assert.equal(moveToOther(l, 0), l) // not split
})

test('closing a pane moves its tabs to the other, which keeps its tab in front', () => {
  const l = sync(undefined, [claude('a'), zsh('b'), zsh('c')], 0.5)
  const r = closePane(l, 1)
  assert.deepEqual(tabs(r), [['a', 'b', 'c']])
  assert.equal(focusedTab(r), 'a')
  const k = closePane(focusPane(l, 1), 0)
  assert.deepEqual(tabs(k), [['b', 'c', 'a']])
  assert.equal(focusedTab(k), 'b')
  assert.equal(k.zoom, false)
})

test('labels take the lowest free number and never renumber', () => {
  let l = sync(undefined, [zsh('a'), zsh('b'), zsh('c')], 0.5)
  assert.deepEqual(l.labels, { a: 'zsh', b: 'zsh 2', c: 'zsh 3' })
  l = sync(l, [zsh('b'), zsh('c')], 0.5)
  l = sync(l, [zsh('b'), zsh('c'), zsh('d'), zsh('e')], 0.5)
  assert.deepEqual(l.labels, { b: 'zsh 2', c: 'zsh 3', d: 'zsh', e: 'zsh 4' })
})

test('cycle wraps around the focused pane', () => {
  const l = one(claude('a'), zsh('b'), zsh('c'))
  assert.equal(focusedTab(cycle(l, -1)), 'c')
  assert.equal(focusedTab(cycle(cycle(l, 1), 1)), 'c')
  assert.equal(focusedTab(cycle(activate(l, 'c'), 1)), 'a')
  const s = sync(undefined, [claude('a'), zsh('b'), zsh('c')], 0.5)
  assert.equal(focusedTab(cycle(focusPane(s, 1), 1)), 'c') // stays in its pane
  assert.equal(focusedTab(cycle(s, 1)), 'a')
})
