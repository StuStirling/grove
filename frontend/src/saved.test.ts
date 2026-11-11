import test from 'node:test'
import assert from 'node:assert/strict'
import { savedLayout, saveLayouts, startLayout } from './saved.ts'
import { sync, type Layout } from './layout.ts'

const memory = () => {
  const m = new Map<string, string>()
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) },
  })
  return m
}

const L: Layout = {
  panes: [{ tabs: ['p3'], active: 'p3' }, { tabs: ['p4', 'p5'], active: 'p5' }],
  focus: 1,
  ratio: 0.6,
  zoom: false,
  labels: { p3: 'claude', p4: 'zsh', p5: 'claude 2' },
}

test('a saved layout round-trips', () => {
  memory()
  saveLayouts({ '/a': L })
  assert.deepEqual(savedLayout('/a'), L)
})

test('bad or unreadable saved data gives no layout', () => {
  const m = memory()
  m.set('grove.layout./a', '{')
  assert.equal(savedLayout('/a'), undefined)
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => { throw new Error('denied') } } })
  assert.equal(savedLayout('/a'), undefined)
})

test('startLayout prefers memory, and uses the saved layout only for a restored worktree', () => {
  memory()
  saveLayouts({ '/a': L })
  const inMemory = { ...L, ratio: 0.3 }
  assert.equal(startLayout(inMemory, true, '/a'), inMemory)
  assert.equal(startLayout(undefined, false, '/a'), undefined)
  assert.deepEqual(startLayout(undefined, true, '/a'), L)
})

test('restored layout with a missing tab', () => {
  memory()
  saveLayouts({ '/a': L })
  const l = sync(startLayout(undefined, true, '/a'), [{ id: 'p3', name: 'claude' }, { id: 'p4', name: 'zsh' }], 0.5)
  assert.equal(l.panes.length, 2)
  assert.deepEqual(l.panes[1].tabs, ['p4'])
  assert.equal(l.panes[1].active, 'p4')
})
