import test from 'node:test'
import assert from 'node:assert/strict'
import type { main } from '../wailsjs/go/models'
import * as rm from './removal.ts'

const ws = (name: string) => ({ name, branch: name, dir: '/w/' + name, repo: 'r', repoPath: '/r' }) as main.WorkspaceInfo
const res = (status: string, extra: Partial<main.RemoveResult> = {}) => ({ status, reason: '', detail: '', branchKept: '', ...extra }) as main.RemoveResult

test('a clean removal shows done, collapses, then goes', () => {
  let rs = rm.start({}, ws('a'), 0)
  assert.equal(rs.a.kind, 'removing')
  assert.equal(rm.usable(rs.a), false)
  rs = rm.result(rs, 'a', res('removed'), 1000)
  assert.deepEqual(rs.a, { ws: rs.a.ws, index: 0, kind: 'done', until: 1000 + rm.DONE_MS, collapsing: false })
  assert.equal(rm.tick(rs, 1000 + rm.DONE_MS - 1), rs, 'nothing due yet')
  rs = rm.tick(rs, 1000 + rm.DONE_MS)
  assert.equal(rs.a.kind === 'done' && rs.a.collapsing, true)
  rs = rm.tick(rs, rm.nextDeadline(rs)!)
  assert.deepEqual(rs, {})
})

test('a kept branch waits, pauses while held and resumes with the time left', () => {
  let rs = rm.result(rm.start({}, ws('a'), 0), 'a', res('removed', { branchKept: 'unmerged' }), 0)
  assert.equal(rs.a.kind === 'kept' && rs.a.reason, 'unmerged')
  assert.equal(rm.nextDeadline(rs), rm.KEPT_MS)
  rs = rm.hold(rs, 'a', true, 4000)
  assert.equal(rm.nextDeadline(rs), null, 'paused')
  assert.equal(rm.tick(rs, 60_000), rs, 'a paused row never expires')
  assert.equal(rm.hold(rs, 'a', true, 5000), rs, 'holding twice changes nothing')
  rs = rm.hold(rs, 'a', false, 9000)
  assert.equal(rm.nextDeadline(rs), 9000 + rm.KEPT_MS - 4000)
  assert.deepEqual(rm.tick(rs, 9000 + rm.KEPT_MS - 4000), {})
})

test('dismiss drops a kept row only', () => {
  const removing = rm.start({}, ws('a'), 0)
  assert.equal(rm.dismiss(removing, 'a'), removing)
  const kept = rm.result(removing, 'a', res('removed', { branchKept: 'unmerged' }), 0)
  assert.deepEqual(rm.dismiss(kept, 'a'), {})
})

test('delete branch: kept, deleting, then done or back to kept with the error', () => {
  const kept = rm.result(rm.start({}, ws('a'), 0), 'a', res('removed', { branchKept: 'unmerged' }), 0)
  const deleting = rm.deleting(kept, kept.a)
  assert.equal(deleting.a.kind, 'deleting')
  assert.equal(rm.usable(deleting.a), false)
  assert.equal(rm.nextDeadline(deleting), null)
  assert.equal(rm.branchResult(deleting, 'a', '', 50).a.kind, 'done')
  const failed = rm.branchResult(deleting, 'a', "branch 'a' is checked out", 50)
  assert.equal(failed.a.kind === 'kept' && failed.a.reason, "branch 'a' is checked out")
  assert.equal(rm.nextDeadline(failed), 50 + rm.KEPT_MS, 'a fresh wait')
})

test('delete branch after the row timed out behind the confirm brings it back', () => {
  const kept = rm.result(rm.start({}, ws('a'), 3), 'a', res('removed', { branchKept: 'unmerged' }), 0)
  const expired = rm.tick(kept, rm.KEPT_MS)
  assert.deepEqual(expired, {})
  const rs = rm.deleting(expired, kept.a)
  assert.equal(rs.a.kind, 'deleting')
  assert.equal(rs.a.index, 3)
})

test('a failed removal can be forced or kept', () => {
  const failed = rm.result(rm.start({}, ws('a'), 0), 'a', res('dirty', { reason: '2 uncommitted changes', detail: 'fatal: …' }), 0)
  assert.deepEqual(failed.a, { ws: failed.a.ws, index: 0, kind: 'failed', reason: '2 uncommitted changes', detail: 'fatal: …', dirty: true })
  assert.equal(rm.usable(failed.a), true, 'the worktree is still there')
  const other = rm.result(rm.start({}, ws('a'), 0), 'a', res('failed', { reason: 'nope' }), 0)
  assert.equal(other.a.kind === 'failed' && other.a.dirty, false)

  assert.equal(rm.force(failed, 'a').a.kind, 'removing')
  assert.deepEqual(rm.keep(failed, 'a'), {})
  const removing = rm.force(failed, 'a')
  assert.equal(rm.keep(removing, 'a'), removing, 'keep only applies to a failure')
  assert.equal(rm.force(removing, 'a'), removing, 'force only applies to a failure')
})

test('a result for a row that was dismissed is ignored', () => {
  assert.deepEqual(rm.result({}, 'a', res('removed'), 0), {})
  assert.deepEqual(rm.branchResult({}, 'a', '', 0), {})
  const kept = rm.result(rm.start({}, ws('a'), 0), 'a', res('removed', { branchKept: 'unmerged' }), 0)
  assert.equal(rm.result(kept, 'a', res('failed'), 0), kept, 'only a removing row takes a result')
})

test('several removals at once stay independent', () => {
  let rs = rm.start(rm.start(rm.start({}, ws('a'), 0), ws('b'), 1), ws('c'), 2)
  rs = rm.result(rs, 'b', res('dirty', { reason: '1 uncommitted change' }), 10)
  rs = rm.result(rs, 'a', res('removed', { branchKept: 'unmerged' }), 20)
  assert.deepEqual([rs.a.kind, rs.b.kind, rs.c.kind], ['kept', 'failed', 'removing'])
  rs = rm.force(rs, 'b')
  rs = rm.dismiss(rs, 'a')
  assert.deepEqual(Object.keys(rs).sort(), ['b', 'c'])
  rs = rm.result(rs, 'c', res('removed'), 30)
  rs = rm.result(rs, 'b', res('failed', { reason: 'locked' }), 40)
  assert.deepEqual([rs.b.kind, rs.c.kind], ['failed', 'done'])
  rs = rm.tick(rs, 40 + rm.DONE_MS)
  assert.equal(rs.b.kind, 'failed', "c's timer leaves b alone")
})

test('removed rows go back where they were', () => {
  const [a, b, c, d] = ['a', 'b', 'c', 'd'].map(ws)
  const rs = rm.start(rm.start({}, b, 1), d, 3)
  const names = (l: main.WorkspaceInfo[]) => l.map((w) => w.name).join('')
  assert.equal(names(rm.withRemovals([a, c], rs)), 'abcd')
  assert.equal(names(rm.withRemovals([a, b, c], rs)), 'abcd', 'only rows missing from the list come back')
  assert.equal(names(rm.withRemovals([], rs)), 'bd', 'positions past the end clamp')
})
