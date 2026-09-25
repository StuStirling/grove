import test from 'node:test'
import assert from 'node:assert/strict'
import type { main } from '../wailsjs/go/models'
import { groups, removeText } from './repos.ts'

const repo = (path: string, added = false) => ({ name: path.slice(1), path, added }) as main.RepoInfo
const ws = (repoPath: string, name: string, kinds: string[] = []) =>
  ({ name, branch: 'b-' + name, dir: `${repoPath || '/manual'}/${name}`, repoPath, panes: kinds.map((kind, i) => ({ id: name + i, kind })) }) as main.WorkspaceInfo
const shape = (gs: ReturnType<typeof groups>) => gs.map((g) => [g.repo?.path ?? null, g.rows.map((w) => w.dir)])

const [a, b] = [repo('/a'), repo('/b', true)]
const rows = [ws('/a', 'main'), ws('/a', 'fix'), ws('/b', 'main'), ws('', 'notes')]

test('one group per repo in the window order, even alone, then the manual entries', () => {
  assert.deepEqual(shape(groups(rows, [a, b], [], '')), [
    ['/a', ['/a/main', '/a/fix']],
    ['/b', ['/b/main']],
    [null, ['/manual/notes']],
  ])
  assert.deepEqual(shape(groups(rows.slice(0, 2), [a], [], '')), [['/a', ['/a/main', '/a/fix']]], 'no manual heading without manual entries')
  assert.deepEqual(shape(groups([ws('/gone', 'x'), ...rows.slice(0, 1)], [a], [], '')), [['/a', ['/a/main']]], "a removed repo's rows go")
})

test('a collapsed group keeps its header but no rows, until the filter has text', () => {
  assert.deepEqual(shape(groups(rows, [a, b], ['/a'], '')), [
    ['/a', []],
    ['/b', ['/b/main']],
    [null, ['/manual/notes']],
  ])
  // Filtering shows matches in collapsed groups, and only groups with a match.
  assert.deepEqual(shape(groups(rows, [a, b], ['/a'], 'fix')), [['/a', ['/a/fix']]])
  assert.deepEqual(shape(groups(rows, [a, b], ['/a'], 'main')), [
    ['/a', ['/a/main']],
    ['/b', ['/b/main']],
  ])
  assert.deepEqual(shape(groups(rows, [a, b], [], 'b-notes')), [[null, ['/manual/notes']]], 'branches match too')
  assert.deepEqual(groups(rows, [a, b], [], 'zzz'), [])
})

test('the remove confirm counts what closes, in the singular and dropping a zero half', () => {
  const text = (...wss: main.WorkspaceInfo[]) => removeText('app', wss)
  assert.equal(text(ws('/a', 'x')).title, 'Remove app from this window?')
  assert.equal(text(ws('/a', 'x'), ws('/a', 'y')).detail, 'Its 2 worktrees and branches stay on disk.', 'nothing open: no second sentence')
  assert.equal(text(ws('/a', 'x', ['claude'])).detail, 'Its 1 worktree and branch stay on disk. 1 open Claude tab will close.')
  assert.equal(text(ws('/a', 'x', ['shell', 'shell'])).detail, 'Its 1 worktree and branch stay on disk. 2 shells will close.')
  assert.equal(
    text(ws('/a', 'x', ['claude', 'shell']), ws('/a', 'y', ['claude']), ws('/a', 'z')).detail,
    'Its 3 worktrees and branches stay on disk. 2 open Claude tabs and 1 shell will close.',
  )
})
