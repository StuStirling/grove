import { test } from 'node:test'
import assert from 'node:assert/strict'
import { nextNeeding } from './attention.ts'

const ws = (dir: string, ...panes: [string, string, number][]) => ({ dir, panes: panes.map(([id, claude, since]) => ({ id, claude, since })) })

test('nextNeeding goes to waiting tabs before finished ones, oldest first', () => {
  const all = [ws('a', ['a1', 'idle', 1], ['a2', 'working', 0]), ws('b', ['b1', 'waiting', 5]), ws('c', ['c1', 'waiting', 3])]
  assert.deepEqual(nextNeeding(all), { dir: 'c', id: 'c1' })
  assert.deepEqual(nextNeeding([all[0]]), { dir: 'a', id: 'a1' })
})

test('nextNeeding skips the tab in front of you, and finds nothing when none want you', () => {
  const all = [ws('a', ['a1', 'waiting', 1]), ws('b', ['b1', 'idle', 2])]
  assert.deepEqual(nextNeeding(all, 'a1'), { dir: 'b', id: 'b1' })
  assert.equal(nextNeeding([ws('a', ['a1', 'working', 1], ['a2', '', 0])]), undefined)
  assert.equal(nextNeeding([{ dir: 'x' }]), undefined)
})
