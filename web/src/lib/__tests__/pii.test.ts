/**
 * C11: maskEmail must agree, case for case, with api/pkg/redact.Email.
 *
 * The audit's finding was three implementations of one masking rule — two in Go,
 * one here — which is why they had already drifted. Consolidation removed the
 * duplicate Go copy; Go and TypeScript cannot share the surviving code, so this
 * test reads the SAME fixture the Go package's test reads. A rule change made on
 * one side of the monorepo now fails on that side until the fixture and the other
 * side follow.
 *
 * Reading across the subtree is deliberate: the alternative is a second copy of
 * the case table here, which is exactly the failure mode C11 reported.
 */

import { readFileSync } from 'fs'
import { join } from 'path'
import { maskEmail } from '@/lib/pii'

const CONTRACT = join(__dirname, '../../../../api/pkg/redact/testdata/email_masking.json')

type Case = { why: string; in: string; out: string }

const cases: Case[] = JSON.parse(readFileSync(CONTRACT, 'utf8')).cases

describe('maskEmail matches the shared Go/TypeScript contract', () => {
  it('has cases to check', () => {
    // A fixture that failed to load would otherwise make this file vacuously green.
    expect(cases.length).toBeGreaterThan(5)
  })

  it.each(cases)('$why', ({ in: input, out }) => {
    expect(maskEmail(input)).toBe(out)
  })
})

describe('maskEmail handles input the Go side cannot receive', () => {
  // The TypeScript signature takes unknown, because a log context value is
  // whatever the caller put there.
  it.each([
    [undefined, '***'],
    [null, '***'],
    [42, '***'],
    [{ email: 'mentee@example.com' }, '***'],
  ])('masks %p entirely', (input, expected) => {
    expect(maskEmail(input)).toBe(expected)
  })
})
