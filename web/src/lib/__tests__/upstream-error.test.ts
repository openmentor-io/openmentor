import { GENERIC_SUBMIT_ERROR, upstreamErrorMessage } from '@/lib/upstream-error'

describe('upstreamErrorMessage', () => {
  // These are the literal strings the Go API returns on the contact and
  // migration paths; the proxy forwards them with their 4xx status.
  it.each([
    [400, 'Captcha verification failed', 'Captcha verification failed.'],
    [
      400,
      "That doesn't look like a getmentor.dev profile link",
      "That doesn't look like a getmentor.dev profile link.",
    ],
    [400, 'Validation failed', 'Validation failed.'],
    [429, 'Too many requests — try again in an hour.', 'Too many requests — try again in an hour.'],
    [409, 'This username is already taken', 'This username is already taken.'],
  ])('surfaces the %i message %s', (status, error, expected) => {
    expect(upstreamErrorMessage(status, { success: false, error })).toBe(expected)
  })

  it('keeps a 2xx body that reports failure without an HTTP error', () => {
    expect(upstreamErrorMessage(200, { success: false, error: 'Nothing to migrate' })).toBe(
      'Nothing to migrate.'
    )
  })

  // A 5xx body is either the proxy's own generic text or, past the proxy, a
  // gateway page — never something to render.
  it.each([500, 502, 503, 504])('never renders the %i body', (status) => {
    expect(upstreamErrorMessage(status, { error: 'Internal server error' })).toBe(
      GENERIC_SUBMIT_ERROR
    )
    expect(upstreamErrorMessage(status, { error: 'pq: relation "mentors" does not exist' })).toBe(
      GENERIC_SUBMIT_ERROR
    )
  })

  it.each([
    ['a body with no error field', { success: false }],
    ['a non-string error', { error: { code: 17 } }],
    ['an empty error', { error: '   ' }],
    ['an unparsed body', null],
    ['no body at all', undefined],
  ])('falls back for %s', (_label, body) => {
    expect(upstreamErrorMessage(400, body)).toBe(GENERIC_SUBMIT_ERROR)
  })

  it('takes a caller-supplied fallback', () => {
    expect(upstreamErrorMessage(500, { error: 'boom' }, 'Could not save.')).toBe('Could not save.')
  })

  it('flattens and truncates so the panel stays one line of prose', () => {
    const message = upstreamErrorMessage(400, {
      error: `Validation failed\n\n${'x'.repeat(500)}`,
    })
    expect(message).toBe(`Validation failed ${'x'.repeat(182)}.`)
    expect(message.length).toBe(201) // 200 of body + the added full stop
  })
})
