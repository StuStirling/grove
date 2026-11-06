import { test } from 'node:test'
import assert from 'node:assert/strict'
import { shellQuote } from './util.ts'

test('shellQuote wraps paths so spaces and quotes survive the shell', () => {
  assert.equal(shellQuote('/tmp/a.png'), "'/tmp/a.png'")
  assert.equal(shellQuote('/tmp/my file.png'), "'/tmp/my file.png'")
  assert.equal(shellQuote("/tmp/it's.png"), "'/tmp/it'\\''s.png'")
})
