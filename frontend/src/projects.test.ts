import test from 'node:test'
import assert from 'node:assert/strict'
import type { main } from '../wailsjs/go/models'
import { loadOverrides, mains, membership, projectNames, rowName, setOverride } from './projects.ts'

const ws = (repoPath: string, folder: string) =>
  ({ name: folder, branch: 'b-' + folder, dir: `${repoPath || '/manual'}/${folder}`, repo: repoPath.slice(1), repoPath }) as main.WorkspaceInfo
const rows = ['/a/main', '/a/eng-1', '/a/spike', '/b/main', '/b/eng-1', '/c/main', '/c/eng-1', '/c/solo'].map((d) => ws(d.slice(0, 2), d.slice(3)))
const sorted = (m: Map<string, string>) => [...m].sort(([x], [y]) => x.localeCompare(y))

test('the same folder in two or more repos is a project; in one repo it is not', () => {
  assert.deepEqual(sorted(membership(rows, {})), [['/a/eng-1', 'eng-1'], ['/b/eng-1', 'eng-1'], ['/c/eng-1', 'eng-1']])
})

test("main checkouts never group by folder, and mains picks each repo's first row", () => {
  assert.deepEqual([...mains(rows)], ['/a/main', '/b/main', '/c/main'])
  assert.equal(membership(rows, {}).has('/a/main'), false)
  assert.deepEqual([...mains([ws('', 'notes'), ...rows.slice(0, 1)])], ['/a/main'], 'manual rows have no main')
})

test('the folder, not the prefixed name, decides', () => {
  const prefixed = [ws('/a', 'main'), { ...ws('/a', 'x'), name: 'android/x' }, ws('/b', 'main'), { ...ws('/b', 'x'), name: 'ios/x' }]
  assert.equal(membership(prefixed, {}).get('/a/x'), 'x')
})

test('an override adds a worktree with another folder name, and counts towards its project', () => {
  assert.equal(membership(rows, { '/c/solo': 'eng-1' }).get('/c/solo'), 'eng-1')
  const two = [ws('/a', 'main'), ws('/a', 'eng-1'), ws('/b', 'main'), ws('/b', 'other')]
  assert.equal(membership(two, { '/b/other': 'eng-1' }).get('/a/eng-1'), 'eng-1', 'the override member makes two repos')
})

test('a null override removes a derived member; a project left in one repo dissolves', () => {
  const two = rows.filter((w) => w.repoPath !== '/c')
  assert.equal(membership(two, { '/b/eng-1': null }).size, 0)
})

test('a project made by override may have one member', () => {
  assert.deepEqual([...membership(rows, { '/a/spike': 'research' })].filter(([, p]) => p === 'research'), [['/a/spike', 'research']])
  assert.deepEqual(projectNames(membership(rows, { '/a/spike': 'research' })), ['eng-1', 'research'])
})

test('manual rows are never members, even with an override', () => {
  assert.equal(membership([...rows, ws('', 'eng-1')], { '/manual/eng-1': 'eng-1' }).has('/manual/eng-1'), false)
})

test('rowName shows the repo, and the folder when it differs from the project', () => {
  assert.equal(rowName(ws('/a', 'eng-1'), 'eng-1'), 'a')
  assert.equal(rowName(ws('/a', 'spike'), 'eng-1'), 'a · spike')
})

test('corrupt saved overrides load as empty', () => {
  const stored = (v: string) => Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => v, setItem() {} } })
  stored('{nope')
  assert.deepEqual(loadOverrides(), {})
  stored('[1]')
  assert.deepEqual(loadOverrides(), {})
  stored('{"/a/spike":"x"}')
  assert.deepEqual(loadOverrides(), { '/a/spike': 'x' })
  stored('{"/a/spike":"x","/a/eng-1":null,"/b/eng-1":5,"/c/eng-1":true}')
  assert.deepEqual(loadOverrides(), { '/a/spike': 'x', '/a/eng-1': null }, 'values that are not a name or null are dropped')
})

test("setOverride writes one worktree's change over what is stored, keeping other windows' entries", () => {
  let saved = '{"/other/window":"theirs"}'
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: () => saved, setItem: (_: string, v: string) => (saved = v) } })
  assert.deepEqual(setOverride('/a/spike', 'mine'), { '/other/window': 'theirs', '/a/spike': 'mine' })
  assert.deepEqual(JSON.parse(saved), { '/other/window': 'theirs', '/a/spike': 'mine' })
})
