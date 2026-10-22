import test from 'node:test'
import assert from 'node:assert/strict'
import type { main } from '../wailsjs/go/models'
import { buildPrompt, clashes, defaultRepos, existingWorktree, latest, nextLead, slug, suggestBranch, suggestName } from './start.ts'

const issue = {
  id: 'ENG-412',
  title: 'Let people snooze',
  description: 'Pause pushes for a while.',
  url: 'https://linear.app/acme/issue/ENG-412',
  branchName: 'stu/eng-412-let-people-snooze',
  state: 'In Progress',
  stateType: 'started',
  team: 'ENG',
  project: 'Notification controls',
} as main.LinearIssue
const project = {
  id: 'p1',
  name: 'Notification controls',
  description: 'Finer control.',
  content: 'Snooze and quiet hours.',
  url: 'https://linear.app/acme/project/nc',
  status: 'In Progress',
  statusType: 'started',
  targetDate: '2026-11-14',
  progress: 0.43,
  issues: [
    issue,
    { ...issue, id: 'ENG-401', title: 'Store preferences', description: 'Old work', state: 'Done', stateType: 'completed', branchName: 'stu/eng-401-store' },
    { ...issue, id: 'ENG-420', title: 'Profile API', description: 'x'.repeat(700), state: 'Todo', stateType: 'unstarted' },
  ],
} as main.LinearProject
const api = { repo: 'api', path: '/w/api/eng-412' }
const ios = { repo: 'ios', path: '/w/ios/eng-412' }

test('an issue in one repo', () => {
  assert.equal(
    buildPrompt({ kind: 'issue', issue }, [api], issue.branchName),
    `Plan the work for Linear issue ENG-412: Let people snooze
https://linear.app/acme/issue/ENG-412

You're in the api worktree on branch stu/eng-412-let-people-snooze.

## Description
Pause pushes for a while.`,
  )
})

test('an issue across repos lists every worktree, the lead first', () => {
  assert.equal(
    buildPrompt({ kind: 'issue', issue }, [api, ios], issue.branchName),
    `Plan the work for Linear issue ENG-412: Let people snooze
https://linear.app/acme/issue/ENG-412

You're in the api worktree. The work spans 2 repos, each with a worktree on branch stu/eng-412-let-people-snooze:
- api (here): /w/api/eng-412
- ios: /w/ios/eng-412

Plan across every repo before changing anything.

## Description
Pause pushes for a while.`,
  )
})

test('an issue with no description has no empty heading', () => {
  const p = buildPrompt({ kind: 'issue', issue: { ...issue, description: '' } as main.LinearIssue }, [api], issue.branchName)
  assert.ok(p.endsWith('on branch stu/eng-412-let-people-snooze.'), p)
})

test('a project lists all its issues, done ones without descriptions', () => {
  assert.equal(
    buildPrompt({ kind: 'project', project }, [api, ios], 'stu/notification-controls'),
    `Make an end-to-end plan for the Linear project "Notification controls".
https://linear.app/acme/project/nc

You're in the api worktree. The work spans 2 repos, each with a worktree on branch stu/notification-controls:
- api (here): /w/api/eng-412
- ios: /w/ios/eng-412

Cover the whole stack: what changes in each repo, the contracts between them, and the order to ship in. Map each step to the issues below.

## Project
Finer control.

Snooze and quiet hours.

## Issues (done ones are already shipped: build on them, don't plan them again)
- ENG-412 Let people snooze [In Progress]
  Pause pushes for a while.
- ENG-401 Store preferences [Done]
- ENG-420 Profile API [Todo]
  ${'x'.repeat(600)}…`,
  )
})

test('an empty project has no empty headings', () => {
  const p = buildPrompt({ kind: 'project', project: { ...project, description: '', content: '', issues: [] } as main.LinearProject }, [api], 'b')
  assert.ok(p.endsWith('Map each step to the issues below.'), p)
})

test('slugs, branches and names', () => {
  assert.equal(slug('Notification controls!'), 'notification-controls')
  assert.equal(slug('Café ☕ launch'), 'caf-launch')
  assert.equal(slug('☕'), 'work')
  assert.equal(slug('a'.repeat(80)).length, 60)
  assert.equal(suggestBranch({ kind: 'issue', issue }), 'stu/eng-412-let-people-snooze')
  assert.equal(suggestBranch({ kind: 'project', project }), 'stu/notification-controls')
  assert.equal(suggestBranch({ kind: 'project', project: { ...project, issues: [] } as main.LinearProject }), 'notification-controls')
  assert.equal(suggestName({ kind: 'issue', issue }), 'eng-412')
  assert.equal(suggestName({ kind: 'project', project }), 'notification-controls')
})

test('default repos: the last set still in the window, else the current repo, else the first', () => {
  const repos = [{ path: '/a', name: 'a' }, { path: '/b', name: 'b' }, { path: '/c', name: 'c' }] as main.RepoInfo[]
  assert.deepEqual(defaultRepos(['/c', '/gone', '/a'], repos, '/b'), ['/a', '/c'])
  assert.deepEqual(defaultRepos([], repos, '/b'), ['/b'])
  assert.deepEqual(defaultRepos(['/gone'], repos, ''), ['/a'])
})

test('the lead moves to the first ticked repo when unticked', () => {
  assert.equal(nextLead('/b', ['/a', '/b']), '/b')
  assert.equal(nextLead('/b', ['/a']), '/a')
  assert.equal(nextLead('/b', []), '')
})

test('two repos planning the same folder clash', () => {
  assert.deepEqual(
    clashes([
      { repo: 'api', path: '/w/x' },
      { repo: 'ios', path: '/w/x' },
      { repo: 'web', path: '/w/y' },
      { repo: 'and', path: '' },
    ]),
    { api: 'same folder as ios', ios: 'same folder as api' },
  )
})

test("an issue's existing worktree is found by branch, never by an empty one", () => {
  const ws = [
    { dir: '/w/detached', branch: '' },
    { dir: '/w/eng-412', branch: 'stu/eng-412-let-people-snooze' },
  ] as main.WorkspaceInfo[]
  assert.equal(existingWorktree('stu/eng-412-let-people-snooze', ws)?.dir, '/w/eng-412')
  assert.equal(existingWorktree('', ws), undefined)
})

test('only the latest request counts', () => {
  const s = latest()
  const a = s.next()
  const b = s.next()
  assert.equal(s.current(a), false)
  assert.equal(s.current(b), true)
})
