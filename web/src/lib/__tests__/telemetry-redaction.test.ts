/**
 * SENTINEL tests for P14: one review capability and one magic-link token are fed
 * through every telemetry sink the frontend owns — logs, span attributes and
 * analytics properties — and must not come out of any of them.
 */

import { PassThrough } from 'stream'
import winston from 'winston'
import { redactFaroItem } from '@/lib/faro'
import { redactSensitiveEvent } from '@/lib/posthog'
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
const REDACTED_VALUE = '[REDACTED]'

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

  it('exempts PostHog reserved identifiers without exempting session credentials', () => {
    // `$session_id` normalizes to `sessionid`, which the `session` rule matched.
    for (const key of [
      '$session_id',
      '$window_id',
      '$session_entry_url',
      '$session_entry_utm_source',
      '$session_recording_start_reason',
      '$feature_flag_request_id',
    ]) {
      expect(isSensitiveKey(key)).toBe(false)
    }
    // The `$` sigil is the whole safety margin: an application session
    // credential normalizes identically and must still be redacted.
    for (const key of ['sessionid', 'session_id', 'session_token', 'SessionToken', 'cookie']) {
      expect(isSensitiveKey(key)).toBe(true)
    }
    expect(redactValue({ cookie: `sessionid=${LOGIN_TOKEN}` })).toEqual({ cookie: REDACTED_VALUE })
    expect(redactQueryValues(`?sessionid=${LOGIN_TOKEN}`)).toBe('?sessionid=[REDACTED]')
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

  it('redacts a parameter literally named key', () => {
    expect(isSensitiveKey('key')).toBe(true)
    expect(isSensitiveKey('keywords')).toBe(false)
    expect(redactQueryValues('?key=s3cr3t&page=2')).toBe('?key=[REDACTED]&page=2')
    expect(redactQueryValues('?monkey=curious')).toBe('?monkey=curious')
  })

  it('catches a token embedded in free text, not just in a query string', () => {
    // What an upstream failure looks like by the time it reaches winston's message.
    expect(redactQueryValues(`Error: rejected login_token=${LOGIN_TOKEN}`)).toBe(
      'Error: rejected login_token=[REDACTED]'
    )
    expect(redactValue(`verify failed: token=${LOGIN_TOKEN}`)).toBe(
      'verify failed: token=[REDACTED]'
    )
  })

  it('strips a capability PATH from free text, where there is no key= to match', () => {
    // GoApiClient.request builds this message itself on timeout, and logError
    // sends it (and the stack) as an arbitrary string, not as a url field.
    expect(
      redactValue(`Go API request timeout after 10000ms: /api/v1/reviews/${REVIEW_CAPABILITY}/check`)
    ).toBe('Go API request timeout after 10000ms: /api/v1/reviews/:id/check')
    // Under a key that is not on the URL-valued list either.
    expect(redactValue({ detail: `see /mentor/requests/${REVIEW_CAPABILITY}` })).toEqual({
      detail: 'see /mentor/requests/:id',
    })
  })

  it('keeps a matching key that is only three characters from firing on a word', () => {
    // `otp` as a bare substring also matches botpageview / notpurchased, and — the
    // reason it matters — base64 that happens to contain "otp" and end in `=`
    // padding, which would corrupt a replay asset.
    expect(redactQueryValues('?otp=123456&otp_code=7')).toBe('?otp=[REDACTED]&otp_code=[REDACTED]')
    expect(redactQueryValues('?botpageview=1&notpurchased=2')).toBe(
      '?botpageview=1&notpurchased=2'
    )
  })

  it('keeps numbers and booleans under capability-shaped keys', () => {
    // The same exemption the analytics blocklists make: a capability is always a
    // string, and sessions_count is a first-class domain field.
    expect(redactValue({ has_token: false, sessions_count: 12, authorized: true })).toEqual({
      has_token: false,
      sessions_count: 12,
      authorized: true,
    })
    // A string under the same key is still a credential.
    expect(redactValue({ session_token: LOGIN_TOKEN })).toEqual({ session_token: '[REDACTED]' })
  })

  it('truncates the client IP instead of logging it whole', () => {
    expect(maskIp('203.0.113.42')).toBe('203.0.113.0')
    expect(maskIp('203.0.113.42, 10.0.0.1')).toBe('203.0.113.0')
    expect(maskIp('2001:db8:85a3:8d3:1319:8a2e:370:7348')).toBe('2001:db8:85a3::')
    expect(maskIp('2001:db8::1')).toBe('2001:db8::')
    expect(maskIp('::1')).toBe('::1')
    expect(maskIp(undefined)).toBe('')
  })

  it('masks the IPv4 embedded in an IPv4-mapped IPv6 address', () => {
    // A client reaching a dual-stack socket over IPv4 arrives as ::ffff:<v4>,
    // which node renders in either notation. Both carry all four octets, so
    // neither may pass through on the strength of its `::` prefix.
    expect(maskIp('::ffff:203.0.113.42')).toBe('::ffff:203.0.113.0')
    expect(maskIp('::ffff:cb00:712a')).toBe('::ffff:203.0.113.0')
    expect(maskIp('::FFFF:CB00:712A')).toBe('::ffff:203.0.113.0')
    // The deprecated IPv4-compatible form embeds an address the same way.
    expect(maskIp('::cb00:712a')).toBe('::203.0.113.0')
    // Some proxies bracket the address; it is still the same address.
    expect(maskIp('[::ffff:203.0.113.42]')).toBe('::ffff:203.0.113.0')
  })

  it('masks compressed, uppercase and zone-suffixed IPv6 addresses', () => {
    expect(maskIp('2001:DB8:85A3::8A2E:370:7348')).toBe('2001:db8:85a3::')
    expect(maskIp('fe80::1%eth0')).toBe('fe80::')
    expect(maskIp('64:ff9b::203.0.113.42')).toBe('64:ff9b::')
    expect(maskIp('::')).toBe('::')
    // Not an address at all: fail closed rather than log the value.
    expect(maskIp('2001:db8:::1')).toBe(REDACTED_VALUE)
    expect(maskIp('not-an-ip')).toBe(REDACTED_VALUE)
    expect(maskIp('203.0.113')).toBe(REDACTED_VALUE)
    expect(maskIp('999.0.113.42')).toBe(REDACTED_VALUE)
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

  it('scrubs the capability out of an error message and its stack', async () => {
    // The real shape: GoApiClient.request builds this message when the review
    // proxy times out, and logError forwards message + stack to Loki. Neither is
    // a url field, so only the free-form rules see them.
    const output = await captureLogs(({ logError }) => {
      const error = new Error(
        `Go API request timeout after 10000ms: /api/v1/reviews/${REVIEW_CAPABILITY}/check`
      )
      error.stack =
        `Error: ${error.message}\n` +
        '    at GoApiClient.request (/app/src/lib/go-api-client.ts:120:15)\n' +
        `    at checkReview (/app/src/pages/api/reviews/check.ts:40:7) /api/v1/reviews/${REVIEW_CAPABILITY}/check`
      logError(error, { context: 'check-review-proxy' })
    })

    expectClean(output)
    // Still diagnosable: the route, the timeout and the frames all survive.
    expect(output).toContain('/api/v1/reviews/:id/check')
    expect(output).toContain('Go API request timeout after 10000ms')
    expect(output).toContain('go-api-client.ts:120:15')
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

describe('posthog before_send', () => {
  it('scrubs person properties, not just event properties', () => {
    // posthog-js merges the initial props into $set_once on $identify, and $set /
    // $set_once are top-level siblings of properties — a capability landing there
    // becomes a PERSON property that outlives the event.
    const event = redactSensitiveEvent({
      uuid: 'evt-1',
      event: '$identify',
      properties: { $current_url: `https://openmentor.io/reviews/new?request_id=${REVIEW_CAPABILITY}` },
      $set: { login_token: LOGIN_TOKEN },
      $set_once: {
        $initial_current_url: `https://openmentor.io/mentor/auth/callback?token=${LOGIN_TOKEN}`,
        $initial_referring_domain: 'openmentor.io',
      },
    })

    expectClean(JSON.stringify(event))
    expect(event?.$set_once?.$initial_referring_domain).toBe('openmentor.io')
  })

  it('scrubs the capability from autocaptured urls, where it sits in the PATH', () => {
    // /mentor/requests/<uuid> carries the capability with no `key=` in sight, so
    // both the property-name blocklist and the query-pair rule miss it.
    const event = redactSensitiveEvent({
      uuid: 'evt-3',
      event: '$autocapture',
      properties: {
        $current_url: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}?tab=notes`,
        $pathname: `/mentor/requests/${REVIEW_CAPABILITY}`,
        $referrer: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
        $elements_chain: `a:href="/mentor/requests/${REVIEW_CAPABILITY}"nth-child="1"`,
        $host: 'openmentor.io',
      },
      $set_once: {
        $initial_current_url: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
        $initial_referrer: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
      },
    })

    expectClean(JSON.stringify(event))
    // The route still identifies the page, so funnels and heatmaps survive.
    expect(event?.properties.$pathname).toBe('/mentor/requests/:id')
    expect(event?.properties.$current_url).toBe('https://openmentor.io/mentor/requests/:id?tab=notes')
    expect(event?.properties.$host).toBe('openmentor.io')
  })

  it('scrubs the anchor href autocapture nests in $elements', () => {
    // Verbatim shape posthog-js 1.409.5 hands before_send on a click, captured by
    // driving real autocapture over an anchor in jsdom. $elements is an ARRAY, so
    // the top-level string sweep skipped it entirely while $elements_chain — the
    // same data as a string — was redacted.
    const event = redactSensitiveEvent({
      uuid: 'evt-8',
      event: '$autocapture',
      properties: {
        $event_type: 'click',
        $elements: [
          {
            tag_name: 'a',
            $el_text: 'Open request',
            classes: ['card'],
            attr__href: `/mentor/requests/${REVIEW_CAPABILITY}`,
            attr__class: 'card',
            nth_child: 1,
          },
          { tag_name: 'div', nth_child: 1 },
        ],
        $elements_chain: `a.card:attr__href="/mentor/requests/${REVIEW_CAPABILITY}"text="Open request"`,
      },
    } as never)

    expectClean(JSON.stringify(event))
    const elements = event?.properties.$elements as Array<Record<string, unknown>>
    // The element still identifies what was clicked, so autocapture stays useful.
    expect(elements[0].attr__href).toBe('/mentor/requests/:id')
    expect(elements[0].tag_name).toBe('a')
    expect(elements[0].$el_text).toBe('Open request')
    expect(elements[0].attr__class).toBe('card')
    expect(elements[1].tag_name).toBe('div')
  })

  it('terminates on a cyclic property instead of freezing the tab', () => {
    // The nested walk reaches arbitrary caller-supplied properties, not only the
    // JSON-shaped snapshot batch, so a cycle has to end the traversal.
    const node: Record<string, unknown> = {
      href: `/mentor/requests/${REVIEW_CAPABILITY}`,
    }
    node.self = node

    const event = redactSensitiveEvent({
      uuid: 'evt-11',
      event: 'custom_event',
      properties: { nested: node },
    } as never)

    expectClean(JSON.stringify(event?.properties.nested, (_key, value) =>
      value === node ? '[cycle]' : value
    ))
    expect((event?.properties.nested as Record<string, unknown>).href).toBe('/mentor/requests/:id')
  })

  it('keeps the project api key, which posthog-js authenticates the batch with', () => {
    // posthog-js derives the capture body's `api_key` from properties.token, so
    // the `token` key rule rewriting it would have made PostHog reject every
    // batch — the whole analytics pipeline, not one property.
    process.env.NEXT_PUBLIC_POSTHOG_KEY = 'phc_projectkey_sentinel'
    const event = redactSensitiveEvent({
      uuid: 'evt-9',
      event: '$pageview',
      properties: {
        token: 'phc_projectkey_sentinel',
        $current_url: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
      },
    } as never)

    expect(event?.properties.token).toBe('phc_projectkey_sentinel')
    expectClean(JSON.stringify(event))

    // The exemption is by VALUE: any other token under the same key is still a
    // credential, including a magic-link token the SDK never put there.
    const other = redactSensitiveEvent({
      uuid: 'evt-10',
      event: '$pageview',
      properties: { token: LOGIN_TOKEN },
    } as never)
    expect(other?.properties.token).toBe(REDACTED_VALUE)
  })

  it('scrubs the replay first_url out of a snapshot batch', () => {
    // PostHog derives a recording's first_url from the rrweb Meta event's href
    // and the $pageview custom event's payload.href, both nested inside
    // $snapshot_data where the property sweep cannot see them.
    const event = redactSensitiveEvent({
      uuid: 'evt-4',
      event: '$snapshot',
      properties: {
        $session_id: 'sess-1',
        $snapshot_data: [
          {
            type: 4,
            data: { href: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`, width: 1, height: 2 },
          },
          {
            type: 5,
            data: { tag: '$pageview', payload: { href: `/mentor/requests/${REVIEW_CAPABILITY}` } },
          },
          { type: 3, data: { source: 2, id: 7 } },
        ],
      },
    })

    expectClean(JSON.stringify(event))
    const batch = event?.properties.$snapshot_data as Array<{ data: Record<string, unknown> }>
    expect(batch[0].data.href).toBe('https://openmentor.io/mentor/requests/:id')
    expect((batch[1].data.payload as { href: string }).href).toBe('/mentor/requests/:id')
    // Non-URL rrweb events are left exactly as recorded.
    expect(batch[2].data).toEqual({ source: 2, id: 7 })
  })

  it('scrubs hrefs inside the serialized DOM and its mutations', () => {
    // A full snapshot (type 2) carries the rendered anchors, and an incremental
    // mutation (type 3) carries added nodes and attribute changes. Inspecting only
    // the Meta and Custom events left the capability in every replay.
    const event = redactSensitiveEvent({
      uuid: 'evt-5',
      event: '$snapshot',
      properties: {
        $session_id: '0198f0aa-1111-7000-8000-aaaaaaaaaaaa',
        $snapshot_data: [
          {
            type: 2,
            data: {
              node: {
                type: 1,
                childNodes: [
                  {
                    type: 2,
                    tagName: 'a',
                    attributes: { href: `/mentor/requests/${REVIEW_CAPABILITY}`, class: 'btn' },
                    childNodes: [{ type: 3, textContent: 'Open request' }],
                  },
                  {
                    type: 2,
                    tagName: 'img',
                    // A base64 asset must survive byte-identical: running the
                    // `key=value` rule over its `=` padding would corrupt playback.
                    attributes: { src: 'data:image/png;base64,iVBORw0KGgotp/8AAAAA==' },
                  },
                ],
              },
            },
          },
          {
            type: 3,
            data: {
              source: 0,
              adds: [
                {
                  node: {
                    type: 2,
                    tagName: 'a',
                    attributes: { href: `https://openmentor.io/reviews/new?request_id=${REVIEW_CAPABILITY}` },
                  },
                },
              ],
              attributes: [{ id: 9, attributes: { href: `/mentor/requests/${REVIEW_CAPABILITY}` } }],
            },
          },
        ],
      },
    })

    expectClean(JSON.stringify(event))

    type Attributes = Record<string, string>
    const batch = event?.properties.$snapshot_data as [
      { data: { node: { childNodes: Array<{ attributes: Attributes }> } } },
      {
        data: {
          adds: Array<{ node: { attributes: Attributes } }>
          attributes: Array<{ id: number; attributes: Attributes }>
        }
      },
    ]

    const nodes = batch[0].data.node.childNodes
    expect(nodes[0].attributes.href).toBe('/mentor/requests/:id')
    // Everything that is not a capability is untouched, base64 included.
    expect(nodes[0].attributes.class).toBe('btn')
    expect(nodes[1].attributes.src).toBe('data:image/png;base64,iVBORw0KGgotp/8AAAAA==')

    const mutation = batch[1].data
    expect(mutation.adds[0].node.attributes.href).toBe(
      'https://openmentor.io/reviews/new?request_id=[REDACTED]'
    )
    expect(mutation.attributes[0].attributes.href).toBe('/mentor/requests/:id')
    expect(mutation.attributes[0].id).toBe(9)
  })

  it('keeps the PostHog session id, which is what stitches a session together', () => {
    // The `session` key rule matched `$session_id` (it normalizes to `sessionid`),
    // so every event carried the SAME redacted id: no session grouping, no replay
    // correlation, no exception-to-session link.
    const first = redactSensitiveEvent({
      uuid: 'evt-6',
      event: '$pageview',
      properties: {
        $session_id: '0198f0aa-1111-7000-8000-aaaaaaaaaaaa',
        $window_id: '0198f0aa-2222-7000-8000-bbbbbbbbbbbb',
        $session_entry_url: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
        $session_recording_start_reason: 'sampling_override',
      },
    })
    const second = redactSensitiveEvent({
      uuid: 'evt-7',
      event: '$pageview',
      properties: { $session_id: '0198f0bb-3333-7000-8000-cccccccccccc' },
    })

    expect(first?.properties.$session_id).toBe('0198f0aa-1111-7000-8000-aaaaaaaaaaaa')
    expect(first?.properties.$window_id).toBe('0198f0aa-2222-7000-8000-bbbbbbbbbbbb')
    expect(second?.properties.$session_id).toBe('0198f0bb-3333-7000-8000-cccccccccccc')
    expect(first?.properties.$session_id).not.toBe(second?.properties.$session_id)
    expect(first?.properties.$session_recording_start_reason).toBe('sampling_override')
    // Exempt from the KEY rule, still swept as a string: shape kept, capability gone.
    expect(first?.properties.$session_entry_url).toBe('https://openmentor.io/mentor/requests/:id')
    expectClean(JSON.stringify(first))
  })

  it('passes an event with no person properties through', () => {
    const event = redactSensitiveEvent({ uuid: 'evt-2', event: '$pageview', properties: {} })
    expect(event).toEqual({ uuid: 'evt-2', event: '$pageview', properties: {} })
    expect(redactSensitiveEvent(null)).toBeNull()
  })
})

describe('faro beforeSend', () => {
  it('scrubs the capability from the paths faro instruments automatically', () => {
    // Faro records page.url / view.name and the frames of every error it catches.
    // On /mentor/requests/<uuid> the capability is a PATH segment, so the
    // query-value rule alone never saw it.
    const item = {
      type: 'exception',
      payload: {
        type: 'TypeError',
        value: 'submit failed',
        stacktrace: {
          frames: [
            {
              filename: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}`,
              function: 'onSubmit',
            },
          ],
        },
      },
      meta: {
        page: { url: `https://openmentor.io/mentor/requests/${REVIEW_CAPABILITY}?tab=notes` },
        view: { name: `/mentor/requests/${REVIEW_CAPABILITY}` },
        // Faro's OWN session id must survive: it is how a Frontend Observability
        // session is stitched together.
        session: { id: 'FARO-SESSION-1' },
      },
    }

    redactFaroItem(item)

    expectClean(JSON.stringify(item))
    expect(item.meta.page.url).toBe('https://openmentor.io/mentor/requests/:id?tab=notes')
    expect(item.meta.view.name).toBe('/mentor/requests/:id')
    expect(item.payload.stacktrace.frames[0].filename).toBe(
      'https://openmentor.io/mentor/requests/:id'
    )
    expect(item.payload.stacktrace.frames[0].function).toBe('onSubmit')
    expect(item.meta.session.id).toBe('FARO-SESSION-1')
  })

  it('still redacts a token faro picked up from a query string', () => {
    const item = {
      meta: { page: { url: `https://openmentor.io/mentor/auth/callback?token=${LOGIN_TOKEN}` } },
      payload: { context: { confirmToken: LOGIN_TOKEN } },
    }

    redactFaroItem(item)

    expectClean(JSON.stringify(item))
    expect(item.payload.context.confirmToken).toBe(REDACTED_VALUE)
  })
})

/**
 * H4 SENTINELS. The review capability is no longer the request id in a query
 * string; it is a random token delivered in a URL FRAGMENT. A fragment is never
 * sent to a server, but `window.location.href` still carries it, and that is what
 * posthog-js reads for `$current_url` / `$initial_current_url` and rrweb for a
 * replay's `first_url`. The page clears the fragment before analytics starts;
 * these tests pin the second line of defence.
 */
describe('review capability in a url fragment', () => {
  const REVIEW_TOKEN = 'rvw_SENTINELtelemetryCapabilityAAAAAAAAAAAAAAAA'
  const CAPABILITY_LINK = `https://openmentor.io/reviews/new#review_token=${REVIEW_TOKEN}`

  function expectNoToken(rendered: string): void {
    expect(rendered).not.toContain(REVIEW_TOKEN)
  }

  it('strips the fragment from a url with no query string', () => {
    // Regression: redactUrl split on `?` only, so a fragment-only URL went to the
    // path rule as one long "path" and the token survived intact.
    const redacted = redactUrl(CAPABILITY_LINK)
    expectNoToken(redacted)
    expect(redacted).toBe('https://openmentor.io/reviews/new#review_token=[REDACTED]')
  })

  it('strips the fragment when a query string already consumed the anchors', () => {
    // `?a=b#review_token=…` leaves the pair rule no `?` or `&` to anchor on, which
    // is why `#` is part of its delimiter class.
    const redacted = redactQueryValues(
      `https://openmentor.io/reviews/new?utm_source=email#review_token=${REVIEW_TOKEN}`
    )
    expectNoToken(redacted)
    expect(redacted).toContain('utm_source=email')
  })

  it('scrubs the capability from every posthog url property', () => {
    const event = redactSensitiveEvent({
      uuid: 'evt-h4',
      event: '$pageview',
      properties: {
        $current_url: CAPABILITY_LINK,
        $pathname: '/reviews/new',
        $referrer: CAPABILITY_LINK,
        $host: 'openmentor.io',
      },
      $set_once: { $initial_current_url: CAPABILITY_LINK },
    })

    expectNoToken(JSON.stringify(event))
    // The route survives, so the review funnel still works.
    expect(event?.properties.$pathname).toBe('/reviews/new')
    expect(event?.properties.$host).toBe('openmentor.io')
  })

  it("scrubs the capability from a session replay's first_url and DOM snapshot", () => {
    const event = redactSensitiveEvent({
      uuid: 'evt-h4-replay',
      event: '$snapshot',
      properties: {
        $session_id: '0198f0cc-4444-7000-8000-dddddddddddd',
        $snapshot_data: [
          { type: 4, data: { href: CAPABILITY_LINK, width: 1280, height: 800 } },
          {
            type: 2,
            data: {
              node: {
                childNodes: [
                  { tagName: 'a', attributes: { href: CAPABILITY_LINK } },
                  { tagName: 'input', attributes: { type: 'hidden', value: REVIEW_TOKEN } },
                ],
              },
            },
          },
        ],
        $session_recording_start_reason: 'sampling_override',
      },
    } as never)

    expectNoToken(JSON.stringify(event))
    // The SDK's own ids must survive, or replay correlation breaks (D52).
    expect(event?.properties.$session_id).toBe('0198f0cc-4444-7000-8000-dddddddddddd')
  })

  it('scrubs the capability from a faro page url and an error message', () => {
    const item = {
      type: 'exception',
      payload: {
        type: 'TypeError',
        value: `failed to submit review from ${CAPABILITY_LINK}`,
      },
      meta: {
        page: { url: CAPABILITY_LINK },
        session: { id: 'FARO-SESSION-2' },
      },
    }

    redactFaroItem(item)

    expectNoToken(JSON.stringify(item))
    expect(item.meta.session.id).toBe('FARO-SESSION-2')
  })

  it('scrubs the capability from a log record and a span attribute', () => {
    expectNoToken(String(redactValue(CAPABILITY_LINK, 'url')))
    expectNoToken(String(redactValue(CAPABILITY_LINK, 'message')))
    expectNoToken(String(redactValue(REVIEW_TOKEN, 'token')))
    expectNoToken(String(redactValue(REVIEW_TOKEN, 'review_token')))
    // A bare token under an innocent key: only the shape rule can find this one.
    expectNoToken(String(redactValue(REVIEW_TOKEN, 'value')))

    const attributes: Record<string, unknown> = { 'url.full': CAPABILITY_LINK }
    redactSpanAttributes(attributes)
    expectNoToken(JSON.stringify(attributes))
  })
})
