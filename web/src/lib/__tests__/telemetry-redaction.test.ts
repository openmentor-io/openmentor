/**
 * SENTINEL tests for P14: one review capability and one magic-link token are fed
 * through every telemetry sink the frontend owns — logs, span attributes and
 * analytics properties — and must not come out of any of them.
 */

import { PassThrough } from 'stream'
import winston from 'winston'
import { isSensitiveKey, maskIp, redactQueryValues, redactUrl, redactValue } from '@/lib/redact'
import {
  RedactingSpanExporter,
  redactSpanAttributes,
  shouldIgnoreIncomingRequest,
} from '@/lib/tracing-redact'

// A live review request_id: whoever holds it can submit a review as that mentee.
const REVIEW_CAPABILITY = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'
// A mentor magic-link token from /mentor/auth/callback?token=...
const LOGIN_TOKEN = 'eyJhbGciOiJIUzI1NiJ9.c2VudGluZWw.s1gn4tur3'

/** Every spelling the value travels in across the codebase and the wire. */
const spellings = (secret: string): Record<string, string> => ({
  request_id: secret,
  requestId: secret,
  'REQUEST-ID': secret,
  client_request_id: secret,
  token: secret,
  login_token: secret,
  confirmToken: secret,
  captchaToken: secret,
})

function expectClean(rendered: string): void {
  expect(rendered).not.toContain(REVIEW_CAPABILITY)
  expect(rendered).not.toContain(LOGIN_TOKEN)
  expect(rendered).not.toContain(encodeURIComponent(LOGIN_TOKEN))
}

describe('redact', () => {
  it('recognizes every spelling of a capability-bearing key', () => {
    for (const key of Object.keys(spellings(REVIEW_CAPABILITY))) {
      expect(isSensitiveKey(key)).toBe(true)
    }
    for (const key of ['id', 'mentor_id', 'slug', 'status', 'route', 'duration_ms']) {
      expect(isSensitiveKey(key)).toBe(false)
    }
  })

  it('strips capabilities from a url in path and query position', () => {
    const reviewCheck = `/api/reviews/check?request_id=${REVIEW_CAPABILITY}`
    const callback = `/mentor/auth/callback?token=${encodeURIComponent(LOGIN_TOKEN)}&next=/mentor`
    const upstream = `http://api:8080/api/v1/reviews/${REVIEW_CAPABILITY}/check`

    expectClean(redactUrl(reviewCheck))
    expectClean(redactUrl(callback))
    expectClean(redactUrl(upstream))

    // Still useful: the route survives, only the values go.
    expect(redactUrl(reviewCheck)).toBe('/api/reviews/check?request_id=[REDACTED]')
    expect(redactUrl(upstream)).toBe('http://api:8080/api/v1/reviews/:id/check')
    expect(redactUrl(callback)).toContain('next=/mentor')
  })

  it('scrubs nested, percent-encoded and camelCase forms in one pass', () => {
    const rendered = JSON.stringify(
      redactValue({
        context: 'check-review-proxy',
        ...spellings(REVIEW_CAPABILITY),
        upstream: {
          url: `/api/reviews/check?request%5Fid=${REVIEW_CAPABILITY}`,
          headers: { cookie: `om_session=${LOGIN_TOKEN}` },
        },
        message: `verify failed for /mentor/auth/callback?token=${LOGIN_TOKEN}`,
        attempts: [`?requestId=${REVIEW_CAPABILITY}`],
        mentor_id: 'mentor-1',
      })
    )

    expectClean(rendered)
    expect(rendered).toContain('check-review-proxy')
    expect(rendered).toContain('mentor-1')
  })

  it('scrubs the autocaptured $current_url both client sinks pass through', () => {
    // What PostHog's before_send and Faro's beforeSend hand to redactQueryValues
    // for a pageview fired before the page strips its own query string.
    const currentUrl = `https://openmentor.io/reviews/new?request_id=${REVIEW_CAPABILITY}`
    const callbackUrl = `https://openmentor.io/mentor/auth/callback?token=${LOGIN_TOKEN}`

    expectClean(redactQueryValues(currentUrl))
    expectClean(redactQueryValues(callbackUrl))
    expect(redactQueryValues(currentUrl)).toBe(
      'https://openmentor.io/reviews/new?request_id=[REDACTED]'
    )
  })

  it('truncates the client IP instead of logging it whole', () => {
    expect(maskIp('203.0.113.42')).toBe('203.0.113.0')
    expect(maskIp('203.0.113.42, 10.0.0.1')).toBe('203.0.113.0')
    expect(maskIp('2001:db8:85a3:8d3:1319:8a2e:370:7348')).toBe('2001:db8:85a3::')
    expect(maskIp('2001:db8::1')).toBe('2001:db8::')
    expect(maskIp('::1')).toBe('::1')
    expect(maskIp(undefined)).toBe('')
  })
})

describe('logger', () => {
  // The logger writes through winston's format chain, so capturing a transport
  // stream is what proves the redaction runs for real callers, not just that the
  // helper works in isolation.
  const captureLogs = async (
    log: (logger: typeof import('@/lib/logger')) => void
  ): Promise<string> => {
    const loggerModule = await import('@/lib/logger')
    const stream = new PassThrough()
    const chunks: string[] = []
    stream.on('data', (chunk: Buffer) => chunks.push(chunk.toString()))

    const transport = new winston.transports.Stream({ stream, format: winston.format.json() })
    loggerModule.default.add(transport)
    try {
      log(loggerModule)
      await new Promise((resolve) => setImmediate(resolve))
    } finally {
      loggerModule.default.remove(transport)
    }
    return chunks.join('')
  }

  it('logs the route, not the capability-bearing url, and masks the IP', async () => {
    const output = await captureLogs(({ logHttpRequest }) => {
      logHttpRequest(
        {
          method: 'GET',
          url: `/api/reviews/check?request_id=${REVIEW_CAPABILITY}`,
          headers: { 'user-agent': 'jest', 'x-forwarded-for': '203.0.113.42' },
        },
        { statusCode: 200 },
        12,
        '/api/reviews/check'
      )
    })

    expectClean(output)
    expect(output).toContain('"route":"/api/reviews/check"')
    expect(output).toContain('"ip":"203.0.113.0"')
    expect(output).not.toContain('203.0.113.42')
  })

  it('scrubs a raw url a caller passes as error context', async () => {
    const output = await captureLogs(({ logError }) => {
      logError(new Error(`upstream 500 for ?token=${LOGIN_TOKEN}`), {
        context: 'check-review-proxy',
        url: `/api/reviews/check?request_id=${REVIEW_CAPABILITY}`,
      })
    })

    expectClean(output)
    expect(output).toContain('check-review-proxy')
  })

  it('scrubs context logger metadata', async () => {
    const output = await captureLogs(({ createContextLogger }) => {
      createContextLogger({ component: 'reviews' }).warn('review check failed', {
        request_id: REVIEW_CAPABILITY,
        nested: { login_token: LOGIN_TOKEN },
      })
    })

    expectClean(output)
    expect(output).toContain('reviews')
  })
})

describe('tracing', () => {
  const fakeSpan = (
    name: string,
    attributes: Record<string, unknown>
  ): { name: string; attributes: Record<string, unknown> } => ({ name, attributes })

  it('drops spans for the auth-callback pages', () => {
    expect(shouldIgnoreIncomingRequest(`/mentor/auth/callback?token=${LOGIN_TOKEN}`)).toBe(true)
    expect(shouldIgnoreIncomingRequest(`/admin/auth/callback?token=${LOGIN_TOKEN}`)).toBe(true)
    expect(
      shouldIgnoreIncomingRequest(`/_next/data/abc/mentor/auth/callback.json?token=${LOGIN_TOKEN}`)
    ).toBe(true)
    expect(shouldIgnoreIncomingRequest('/mentor/login')).toBe(false)
    expect(shouldIgnoreIncomingRequest(undefined)).toBe(false)
  })

  it('scrubs url attributes recorded by the http and undici instrumentations', () => {
    const attributes = {
      'url.path': `/api/v1/reviews/${REVIEW_CAPABILITY}/check`,
      'url.query': `request_id=${REVIEW_CAPABILITY}&status=done`,
      'url.full': `http://api:8080/api/v1/reviews/${REVIEW_CAPABILITY}/check?token=${LOGIN_TOKEN}`,
      'http.target': `/mentor/auth/callback?token=${encodeURIComponent(LOGIN_TOKEN)}`,
      'x-internal-mentors-api-auth-token': LOGIN_TOKEN,
      'http.response.status_code': 200,
    }

    redactSpanAttributes(attributes)

    expectClean(JSON.stringify(attributes))
    expect(attributes['url.query']).toContain('status=done')
    expect(attributes['url.path']).toBe('/api/v1/reviews/:id/check')
    expect(attributes['http.response.status_code']).toBe(200)
  })

  it('scrubs every span on the way to the exporter', () => {
    const exported: unknown[] = []
    const delegate = {
      export: (spans: unknown[], done: (result: { code: number }) => void): void => {
        exported.push(...spans)
        done({ code: 0 })
      },
      shutdown: (): Promise<void> => Promise.resolve(),
    }

    const exporter = new RedactingSpanExporter(delegate as never)
    const spans = [
      fakeSpan(`GET /api/reviews/check?request_id=${REVIEW_CAPABILITY}`, {
        'url.query': `request_id=${REVIEW_CAPABILITY}`,
      }),
      fakeSpan('GET /mentor/auth/callback', {
        'url.full': `https://openmentor.io/mentor/auth/callback?token=${LOGIN_TOKEN}`,
      }),
    ]

    exporter.export(spans as never, () => undefined)

    expect(exported).toHaveLength(2)
    expectClean(JSON.stringify(exported))
    expect(spans[0].name).not.toContain(REVIEW_CAPABILITY)
  })
})
