import test from 'node:test'
import assert from 'node:assert/strict'
import type { main } from '../wailsjs/go/models'
import * as up from './update.ts'

const pr = (state: string, extra: Partial<main.PRInfo> = {}) => ({ number: 123, state, reviewer: '', detail: '', ...extra }) as main.PRInfo
const step = (kind: string, ref: string, count: number) => ({ kind, ref, count }) as main.UpdateStep
const key = (k: string, mods: { metaKey?: boolean; ctrlKey?: boolean } = {}) => ({ key: k, metaKey: false, ctrlKey: false, ...mods })

test('prLine names the PR and its review state', () => {
  assert.equal(up.prLine(pr('reviewed', { reviewer: 'alice' })), 'PR #123 · reviewed by alice')
  assert.equal(up.prLine(pr('unreviewed')), 'PR #123 · not reviewed')
  assert.equal(up.prLine(pr('none', { number: 0 })), 'No PR yet')
  assert.equal(up.prLine(pr('merged')), 'PR #123 · merged') // the plan's warning says it's already merged
  assert.equal(up.prLine(pr('unknown', { detail: 'gh is not installed' })), 'PR state unknown (gh is not installed)')
})

test('stepText follows the strategy', () => {
  assert.equal(up.stepText(step('pull', 'origin/feat', 2), 'rebase'), 'Pull 2 commits from origin/feat')
  assert.equal(up.stepText(step('pull', 'origin/feat', 1), 'merge'), 'Pull 1 commit from origin/feat')
  assert.equal(up.stepText(step('base', 'origin/main', 14), 'rebase'), 'Rebase onto origin/main (14 commits behind)')
  assert.equal(up.stepText(step('base', 'origin/main', 14), 'merge'), 'Merge origin/main in (14 commits behind)')
  assert.equal(up.stepText(step('base', 'origin/main', 1), ''), 'Update from origin/main (1 commit behind)')
  assert.equal(up.stepText(step('ff', 'origin/main', 3), 'rebase'), 'Fast-forward from origin/main (3 commits)')
})

test('push copy matches the kind of push', () => {
  assert.equal(up.pushLabel('push'), 'Push')
  assert.equal(up.pushLabel('force'), 'Force push')
  assert.equal(up.pushLabel('upstream'), 'Push and set upstream')
  assert.equal(up.pushQuestion('force', pr('unreviewed'), 'origin'), 'Your branch has diverged from origin. Force push (with lease) to update PR #123?')
  assert.equal(up.pushQuestion('force', pr('none', { number: 0 }), 'origin'), 'Your branch has diverged from origin. Force push (with lease)?')
  assert.equal(up.pushQuestion('push', pr('reviewed'), 'origin'), 'Push to update PR #123?')
  assert.equal(up.pushQuestion('push', pr('none', { number: 0 }), 'upstream'), 'Push to upstream?')
  assert.equal(up.pushQuestion('upstream', pr('none', { number: 0 }), 'origin'), "This branch isn't on origin yet. Push it and set upstream?")
})

test('doneLine says what ran', () => {
  assert.equal(up.doneLine('rebase', [step('pull', 'origin/feat', 1), step('base', 'origin/main', 2)]), 'Rebased onto origin/main')
  assert.equal(up.doneLine('merge', [step('base', 'origin/main', 2)]), 'Merged origin/main in')
  assert.equal(up.doneLine('rebase', [step('pull', 'origin/feat', 1)]), 'Pulled origin/feat')
  assert.equal(up.doneLine('rebase', [step('ff', 'origin/main', 1)]), 'Fast-forwarded from origin/main')
})

test('keyAction maps each view’s keys', () => {
  assert.equal(up.keyAction('plan', key('Enter'), true), 'update')
  assert.equal(up.keyAction('plan', key('r'), true), 'rebase')
  assert.equal(up.keyAction('plan', key('M'), true), 'merge')
  assert.equal(up.keyAction('plan', key('r', { metaKey: true }), true), null, '⌘R stays reload')
  assert.equal(up.keyAction('plan', key('Escape'), true), 'cancel')
  assert.equal(up.keyAction('push', key('Enter'), true), 'push')
  assert.equal(up.keyAction('push', key('Escape'), true), 'cancel')
  assert.equal(up.keyAction('conflicts', key('Enter'), true), 'continue')
  assert.equal(up.keyAction('conflicts', key('c'), true), 'claude')
  assert.equal(up.keyAction('conflicts', key('Backspace', { metaKey: true }), true), 'abort')
  assert.equal(up.keyAction('conflicts', key('Backspace', { ctrlKey: true }), false), 'abort')
  assert.equal(up.keyAction('conflicts', key('Backspace', { ctrlKey: true }), true), null)
  assert.equal(up.keyAction('conflicts', key('Backspace'), true), null)
  assert.equal(up.keyAction('conflicts', key('Escape'), true), 'cancel')
  assert.equal(up.keyAction('conflicts', key('r'), true), null)
})

test('keyAction leaves Enter and letters to a focused button', () => {
  const onButton = (k: string) => ({ ...key(k), onControl: true })
  assert.equal(up.keyAction('push', onButton('Enter'), true), null, 'Tab to Not now, ↩ must not push')
  assert.equal(up.keyAction('plan', onButton('Enter'), true), null)
  assert.equal(up.keyAction('conflicts', onButton('Enter'), true), null)
  assert.equal(up.keyAction('conflicts', onButton('Escape'), true), 'cancel')
  assert.equal(up.keyAction('conflicts', { ...key('Backspace', { metaKey: true }), onControl: true }, true), 'abort')
})
